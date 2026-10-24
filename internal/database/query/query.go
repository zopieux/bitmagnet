package query

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/fts"
	"github.com/bitmagnet-io/bitmagnet/internal/maps"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"gorm.io/gen"
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResultItem struct {
	QueryStringRank float64
}

type GenericResult[T interface{}] struct {
	TotalCount           uint
	TotalCountIsEstimate bool
	HasNextPage          bool
	Items                []T
	Aggregations         Aggregations
}

type SubQueryFactory = func(context.Context, *dao.Query) SubQuery

// GenericQuery executes queries for any type of search and returns a GenericResult
func GenericQuery[T interface{}](
	_ctx context.Context,
	daoQ *dao.Query,
	option Option,
	tableName string,
	factory SubQueryFactory,
) (GenericResult[T], error) {
	gq := genericQuery[T]{
		daoQ:    daoQ,
		factory: factory,
	}
	builder, optionErr := option(newQueryContext(dbContext{
		q:         daoQ,
		tableName: tableName,
		factory:   factory,
	}))

	if optionErr != nil {
		return gq.result, optionErr
	}

	gq.ctx = builder.createContext(_ctx)
	gq.builder = builder
	gq.doItems()
	gq.doCount()

	if aggs, aggErr := gq.builder.calculateAggregations(gq.ctx); aggErr != nil {
		gq.addError(aggErr)
	} else {
		gq.result.Aggregations = aggs
	}

	return gq.result, errors.Join(gq.errs...)
}

type genericQuery[T interface{}] struct {
	ctx     context.Context
	daoQ    *dao.Query
	factory SubQueryFactory
	builder OptionBuilder
	mtx     sync.Mutex
	errs    []error
	result  GenericResult[T]
}

func (gq *genericQuery[_]) newSubQuery(ctx context.Context, withOrder bool) (SubQuery, error) {
	sq := gq.factory(ctx, gq.daoQ)
	if selectErr := gq.builder.applySelect(sq.UnderlyingDB(), withOrder); selectErr != nil {
		return sq, selectErr
	}

	if preErr := gq.builder.applyPre(sq, withOrder); preErr != nil {
		return sq, preErr
	}

	return sq, nil
}

func (gq *genericQuery[_]) addError(err error) {
	gq.mtx.Lock()
	defer gq.mtx.Unlock()
	gq.errs = append(gq.errs, err)
}

type countCacheEntry struct {
	count     uint
	expiresAt time.Time
}

var (
	unfilteredCountMu    sync.RWMutex
	unfilteredCountCache = make(map[string]countCacheEntry)
)

func (gq *genericQuery[_]) doCount() {
	if gq.builder.withTotalCount() {
		if !gq.builder.HasFilter() {
			unfilteredCountMu.RLock()
			entry, ok := unfilteredCountCache[gq.builder.TableName()]
			unfilteredCountMu.RUnlock()
			if ok && time.Now().Before(entry.expiresAt) {
				gq.result.TotalCount = entry.count
				gq.result.TotalCountIsEstimate = false
				return
			}
		}

		sq, sqErr := gq.newSubQuery(gq.ctx, false)
		if sqErr != nil {
			gq.addError(sqErr)
			return
		}

		var count int64
		if countErr := sq.UnderlyingDB().Count(&count).Error; countErr != nil {
			gq.addError(countErr)
		} else {
			gq.result.TotalCount = uint(count)
			gq.result.TotalCountIsEstimate = false
			if !gq.builder.HasFilter() {
				unfilteredCountMu.Lock()
				unfilteredCountCache[gq.builder.TableName()] = countCacheEntry{
					count:     uint(count),
					expiresAt: time.Now().Add(60 * time.Second),
				}
				unfilteredCountMu.Unlock()
			}
		}
	}
}

// doItems gets the items from the database and sets it to the result
func (gq *genericQuery[T]) doItems() {
	if !gq.builder.hasZeroLimit() || gq.builder.needsNextPage() {
		sq, sqErr := gq.newSubQuery(gq.ctx, true)
		if sqErr != nil {
			gq.addError(sqErr)
			return
		}

		if postErr := gq.builder.applyPost(sq.UnderlyingDB()); postErr != nil {
			gq.addError(postErr)
			return
		}

		var finalItems []T
		if txErr := sq.UnderlyingDB().Find(&finalItems).Error; txErr != nil {
			gq.addError(txErr)
			return
		}

		if gq.builder.hasNextPage(len(finalItems)) {
			gq.result.HasNextPage = true
			finalItems = finalItems[:len(finalItems)-1]
		}

		if len(finalItems) > 0 {
			if cbErr := gq.builder.applyCallbacks(gq.ctx, finalItems); cbErr != nil {
				gq.addError(cbErr)
				return
			}
		}

		gq.result.Items = finalItems
	}
}

type BaseSubQuery interface {
	Count() (int64, error)
	TableName() string
	UnderlyingDB() *gorm.DB
}

type SubQuery interface {
	BaseSubQuery
	Scopes(...GormScope) SubQuery
}

type GenericSubQuery[T BaseSubQuery] struct {
	SubQuery BaseSubQuery
}

func (sq GenericSubQuery[T]) Count() (int64, error) {
	return sq.SubQuery.Count()
}

func (sq GenericSubQuery[T]) TableName() string {
	return sq.SubQuery.TableName()
}

func (sq GenericSubQuery[T]) UnderlyingDB() *gorm.DB {
	return sq.SubQuery.UnderlyingDB()
}

type scoper[T BaseSubQuery] interface {
	Scopes(funcs ...GormScope) T
}

func (sq GenericSubQuery[T]) Scopes(fns ...func(gen.Dao) gen.Dao) SubQuery {
	sq.SubQuery = sq.SubQuery.(scoper[T]).Scopes(fns...)
	return sq
}

type Scope = func(*gorm.DB) error

type GormScope = func(gen.Dao) gen.Dao

type DBContext interface {
	Query() *dao.Query
	TableName() string
	NewSubQuery(context.Context) SubQuery
}

type dbContext struct {
	q         *dao.Query
	tableName string
	factory   SubQueryFactory
}

func (db dbContext) Query() *dao.Query {
	return db.q
}

func (db dbContext) TableName() string {
	return db.tableName
}

func (db dbContext) NewSubQuery(ctx context.Context) SubQuery {
	return db.factory(ctx, db.q)
}

type CallbackContext interface {
	DBContext
	Lock()
	Unlock()
}

type callbackContext struct {
	dbContext
	*sync.Mutex
}

type Callback func(ctx context.Context, cbCtx CallbackContext, results any) error

type OrderByColumn struct {
	clause.OrderByColumn
	RequiredJoins []string
}

type OptionBuilder interface {
	DBContext
	Table(string) OptionBuilder
	Join(...TableJoin) OptionBuilder
	RequireJoin(...string) OptionBuilder
	QueryString(string) OptionBuilder
	Scope(...Scope) OptionBuilder
	Select(...clause.Expr) OptionBuilder
	OrderBy(...OrderByColumn) OptionBuilder
	Limit(uint) OptionBuilder
	Offset(uint) OptionBuilder
	Group(...clause.Column) OptionBuilder
	Facet(...Facet) OptionBuilder
	Preload(...field.RelationField) OptionBuilder
	Callback(...Callback) OptionBuilder
	Context(func(ctx context.Context) context.Context) OptionBuilder
	applySelect(db *gorm.DB, withOrderSelect bool) error
	applyPre(sq SubQuery, withOrderJoins bool) error
	applyPost(*gorm.DB) error
	createFacetsFilterCriteria() (Criteria, error)
	calculateAggregations(context.Context) (Aggregations, error)
	WithTotalCount(bool) OptionBuilder
	WithHasNextPage(bool) OptionBuilder
	WithAggregationBudget(float64) OptionBuilder
	AggregationBudget() float64
	withTotalCount() bool
	applyCallbacks(context.Context, any) error
	hasZeroLimit() bool
	needsNextPage() bool
	hasNextPage(nItems int) bool
	withCurrentFacet(string) OptionBuilder
	createContext(context.Context) context.Context
	HasFilter() bool
}

type optionBuilder struct {
	dbContext
	joins map[string]TableJoin
	//revive:disable-next-line:nested-structs
	requiredJoins     maps.InsertMap[string, struct{}]
	// ftsQuery is an SQLite FTS5 query built from the user's search string.
	// It targets the torrent_contents_fts index (see migrations/00001_init.sql).
	ftsQuery          string
	scopes            []Scope
	selections        []clause.Expr
	groupBy           []clause.Column
	orderBy           []OrderByColumn
	limit             model.NullUint
	nextPage          bool
	offset            uint
	facets            []Facet
	currentFacet      string
	preloads          []field.RelationField
	totalCount        bool
	aggregationBudget float64
	callbacks         []Callback
	contextFn         func(context.Context) context.Context
}

type RawJoin struct {
	Query string
	Args  []interface{}
}

func newQueryContext(dbCtx dbContext) OptionBuilder {
	return optionBuilder{
		dbContext:     dbCtx,
		joins:         make(map[string]TableJoin),
		requiredJoins: maps.NewInsertMap[string, struct{}](),
	}
}

func (b optionBuilder) Table(name string) OptionBuilder {
	b.tableName = name

	return b.Scope(func(db *gorm.DB) error {
		db.Table(name)
		return nil
	})
}

func (b optionBuilder) Join(joins ...TableJoin) OptionBuilder {
	bJoins := make(map[string]TableJoin, len(b.joins))
	for _, j := range b.joins {
		bJoins[j.Table.TableName()] = j
	}

	bRequiredJoins := b.requiredJoins.Copy()

	for _, j := range joins {
		bJoins[j.Table.TableName()] = j

		if j.Required {
			bRequiredJoins.SetKey(j.Table.TableName())
		}
	}

	b.joins = bJoins
	b.requiredJoins = bRequiredJoins

	return b
}

func (b optionBuilder) RequireJoin(names ...string) OptionBuilder {
	bRequiredJoins := b.requiredJoins.Copy()
	for _, name := range names {
		bRequiredJoins.SetKey(name)
	}

	b.requiredJoins = bRequiredJoins

	return b
}

func (b optionBuilder) QueryString(str string) OptionBuilder {
	b.ftsQuery = fts.AppQueryToFtsQuery(str)
	return b
}

func (b optionBuilder) Scope(scopes ...Scope) OptionBuilder {
	b.scopes = append(b.scopes, scopes...)
	return b
}

func (b optionBuilder) Select(selections ...clause.Expr) OptionBuilder {
	b.selections = append(b.selections, selections...)
	return b
}

func (b optionBuilder) Group(columns ...clause.Column) OptionBuilder {
	b.groupBy = append(b.groupBy, columns...)
	return b
}

func (b optionBuilder) OrderBy(columns ...OrderByColumn) OptionBuilder {
	b.orderBy = columns
	return b
}

func (b optionBuilder) Limit(limit uint) OptionBuilder {
	b.limit = model.NewNullUint(limit)
	return b
}

func (b optionBuilder) Offset(offset uint) OptionBuilder {
	b.offset = offset
	return b
}

func (b optionBuilder) Facet(facets ...Facet) OptionBuilder {
	b.facets = append(b.facets, facets...)
	return b
}

func (b optionBuilder) Preload(relations ...field.RelationField) OptionBuilder {
	b.preloads = append(b.preloads, relations...)
	return b
}

func (b optionBuilder) Callback(callbacks ...Callback) OptionBuilder {
	b.callbacks = append(b.callbacks, callbacks...)
	return b
}

func (b optionBuilder) Context(fn func(context.Context) context.Context) OptionBuilder {
	prevFn := b.contextFn
	b.contextFn = func(ctx context.Context) context.Context {
		if prevFn != nil {
			ctx = prevFn(ctx)
		}

		return fn(ctx)
	}

	return b
}

func (b optionBuilder) WithTotalCount(bl bool) OptionBuilder {
	b.totalCount = bl
	return b
}

func (b optionBuilder) WithHasNextPage(bl bool) OptionBuilder {
	b.nextPage = bl
	return b
}

func (b optionBuilder) withTotalCount() bool {
	return b.totalCount
}

func (b optionBuilder) hasZeroLimit() bool {
	return b.limit.Valid && b.limit.Uint == 0
}

func (b optionBuilder) needsNextPage() bool {
	return b.limit.Valid && b.nextPage
}

func (b optionBuilder) hasNextPage(nItems int) bool {
	if !b.nextPage {
		return false
	}

	if !b.limit.Valid {
		return false
	}

	return nItems > int(b.limit.Uint)
}

func (b optionBuilder) WithAggregationBudget(budget float64) OptionBuilder {
	b.aggregationBudget = budget
	return b
}

func (b optionBuilder) AggregationBudget() float64 {
	return b.aggregationBudget
}

func (b optionBuilder) withCurrentFacet(facet string) OptionBuilder {
	b.currentFacet = facet
	return b
}

func (b optionBuilder) createContext(ctx context.Context) context.Context {
	if b.contextFn != nil {
		return b.contextFn(ctx)
	}

	return ctx
}

func (b optionBuilder) HasFilter() bool {
	if b.ftsQuery != "" || len(b.scopes) > 0 {
		return true
	}
	for _, f := range b.facets {
		if len(f.Filter()) > 0 {
			return true
		}
	}
	return false
}

func (b optionBuilder) applySelect(db *gorm.DB, withOrderSelect bool) error {
	var selectQueryParts []string

	selectQueryArgs := make([]interface{}, 0)

	if len(b.selections) == 0 {
		selectQueryParts = append(selectQueryParts, "*")
	} else {
		for _, s := range b.selections {
			selectQueryParts = append(selectQueryParts, s.SQL)
			selectQueryArgs = append(selectQueryArgs, s.Vars...)
		}
	}

	if withOrderSelect {
		for i, orderBy := range b.orderBy {
			alias := "_order_" + strconv.Itoa(i)

			if orderBy.Column.Name == QueryStringRankField {
				rankFragment := "0"
				args := make([]interface{}, 0)

				if b.ftsQuery != "" {
					rankFragment = "(SELECT bm25(torrent_contents_fts) FROM torrent_contents_fts WHERE torrent_contents_fts.rowid = " + b.tableName + ".rowid AND torrent_contents_fts MATCH ?)"
					args = append(args, b.ftsQuery)
				}

				selectQueryParts = append(selectQueryParts, rankFragment+" AS "+alias)
				selectQueryArgs = append(selectQueryArgs, args...)

				break
			} else if orderBy.Column.Alias == "" {
				writer := bytes.NewBuffer(nil)
				db.Statement.QuoteTo(writer, orderBy.Column)
				selectQueryParts = append(selectQueryParts, writer.String()+" AS "+alias)
			}
		}
	}

	db.Select(strings.Join(selectQueryParts, ", "), selectQueryArgs...)

	return nil
}

func (b optionBuilder) applyPre(sq SubQuery, withOrderJoins bool) error {
	for _, s := range b.scopes {
		if err := s(sq.UnderlyingDB()); err != nil {
			return err
		}
	}

	if b.ftsQuery != "" {
		sq.UnderlyingDB().Where(
			b.tableName+".rowid IN (SELECT rowid FROM torrent_contents_fts WHERE torrent_contents_fts MATCH ?)",
			b.ftsQuery,
		)
	}

	requiredJoins := b.requiredJoins.Copy()

	aggC, aggCErr := b.createFacetsFilterCriteria()
	if aggCErr != nil {
		return aggCErr
	}

	rawAggC, rawAggCErr := aggC.Raw(b)
	if rawAggCErr != nil {
		return rawAggCErr
	}

	requiredJoins.SetEntries(rawAggC.Joins.Entries()...)

	if withOrderJoins {
		for _, ob := range b.orderBy {
			for _, j := range ob.RequiredJoins {
				requiredJoins.Set(j, struct{}{})
			}
		}
	}

	joins, joinsErr := extractRequiredJoins(b.tableName, b.joins, requiredJoins)
	if joinsErr != nil {
		return joinsErr
	}

	applyJoins(sq, joins...)
	sq.UnderlyingDB().Where(rawAggC.Query, rawAggC.Args...)

	if len(b.groupBy) > 0 {
		sq.UnderlyingDB().Clauses(clause.GroupBy{
			Columns: b.groupBy,
		})
	}

	return nil
}

func extractRequiredJoins(
	tableName string,
	joins map[string]TableJoin,
	requiredJoins maps.InsertMap[string, struct{}],
) ([]TableJoin, error) {
	resolvedJoins := maps.NewInsertMap[string, TableJoin]()

	var addJoin func(name string) error

	addJoin = func(name string) error {
		if name == tableName {
			return nil
		}

		j, ok := joins[name]
		if !ok {
			return fmt.Errorf("required join not found: %s", name)
		}

		for _, depName := range j.Dependencies.Keys() {
			if err := addJoin(depName); err != nil {
				return err
			}
		}

		resolvedJoins.Set(j.Table.TableName(), j)

		return nil
	}
	for _, joinName := range requiredJoins.Keys() {
		if err := addJoin(joinName); err != nil {
			return nil, err
		}
	}

	return resolvedJoins.Values(), nil
}

func applyJoins(sq SubQuery, joins ...TableJoin) {
	for _, j := range joins {
		join := j

		sq.Scopes(func(dao gen.Dao) gen.Dao {
			switch join.Type {
			case TableJoinTypeInner:
				return dao.Join(join.Table, join.On...)
			case TableJoinTypeLeft:
				return dao.LeftJoin(join.Table, join.On...)
			case TableJoinTypeRight:
				return dao.RightJoin(join.Table, join.On...)
			}

			panic("invalid join type")
		})
	}
}

func (b optionBuilder) applyPost(db *gorm.DB) error {
	if len(b.orderBy) > 0 {
		cols := make([]clause.OrderByColumn, 0, len(b.orderBy))

		for i, orderBy := range b.orderBy {
			alias := orderBy.Column.Alias
			if alias == "" {
				alias = "_order_" + strconv.Itoa(i)
			}

			cols = append(cols, clause.OrderByColumn{
				Column: clause.Column{Name: alias},
				Desc:   orderBy.Desc,
			})
		}

		db.Statement.AddClause(clause.OrderBy{
			Columns: cols,
		})
	}

	if b.limit.Valid {
		limit := int(b.limit.Uint)
		if b.nextPage {
			limit++
		}

		db.Limit(limit)
	}

	db.Offset(int(b.offset))

	for _, p := range b.preloads {
		db.Preload(p.Name(), p)
	}

	return nil
}

func (b optionBuilder) applyCallbacks(ctx context.Context, results any) error {
	cbCtx := callbackContext{
		dbContext: b.dbContext,
		Mutex:     &sync.Mutex{},
	}

	var errs []error

	wg := sync.WaitGroup{}
	wg.Add(len(b.callbacks))

	for _, cb := range b.callbacks {
		go (func(cb Callback) {
			defer wg.Done()
			if err := cb(ctx, cbCtx, results); err != nil {
				cbCtx.Lock()
				defer cbCtx.Unlock()
				errs = append(errs, err)
			}
		})(cb)
	}

	wg.Wait()

	return errors.Join(errs...)
}

package query

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/maps"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"gorm.io/gen/field"
)

type FacetConfig interface {
	Key() string
	Label() string
	Logic() model.FacetLogic
	Filter() FacetFilter
	IsAggregated() bool
	AggregationOption(b OptionBuilder) (OptionBuilder, error)
	TriggersCte() bool
}

type Facet interface {
	FacetConfig
	Values(ctx FacetContext) (map[string]string, error)
	Criteria(filter FacetFilter) []Criteria
}

type GroupedFacet interface {
	Facet
	GroupColumn(ctx DBContext) (field.Field, error)
}

type FacetFilter map[string]struct{}

// Values allows iteration over deterministically sorted filter values, which helps with query caching.
func (f FacetFilter) Values() []string {
	values := make([]string, 0, len(f))
	for v := range f {
		values = append(values, v)
	}

	sort.Strings(values)

	return values
}

func (f FacetFilter) HasKey(key string) bool {
	_, ok := f[key]
	return ok
}

type facetConfig struct {
	key                string
	label              string
	logic              model.FacetLogic
	filter             FacetFilter
	aggregate          bool
	aggregationOptions []Option
	triggersCte        bool
}

type FacetOption func(facetConfig) facetConfig

func NewFacetConfig(options ...FacetOption) FacetConfig {
	c := facetConfig{}
	for _, option := range options {
		c = option(c)
	}

	return c
}

type AggregationItem struct {
	Label      string
	Count      uint
	IsEstimate bool
}

type AggregationItems = map[string]AggregationItem

type AggregationGroup struct {
	Label string
	Logic model.FacetLogic
	Items AggregationItems
}

type Aggregations = maps.StringMap[AggregationGroup]

type FacetContext interface {
	DBContext
	Context() context.Context
	NewAggregationQuery(options ...Option) (SubQuery, error)
}

type facetContext struct {
	optionBuilder OptionBuilder
	ctx           context.Context
}

func (ctx facetContext) Query() *dao.Query {
	return ctx.optionBuilder.Query()
}

func (ctx facetContext) TableName() string {
	return ctx.optionBuilder.TableName()
}

func (ctx facetContext) NewSubQuery(c context.Context) SubQuery {
	return ctx.optionBuilder.NewSubQuery(c)
}

func (ctx facetContext) Context() context.Context {
	return ctx.ctx
}

func (ctx facetContext) NewAggregationQuery(options ...Option) (SubQuery, error) {
	subCtx, subErr := Options(options...)(ctx.optionBuilder)
	if subErr != nil {
		return nil, subErr
	}

	sq := ctx.optionBuilder.NewSubQuery(ctx.Context())

	applyErr := subCtx.applyPre(sq, false)
	if applyErr != nil {
		return nil, applyErr
	}

	return sq, nil
}

func FacetHasKey(key string) FacetOption {
	return func(a facetConfig) facetConfig {
		a.key = key
		return a
	}
}

func FacetHasLabel(label string) FacetOption {
	return func(a facetConfig) facetConfig {
		a.label = label
		return a
	}
}

func FacetUsesLogic(logic model.FacetLogic) FacetOption {
	return func(a facetConfig) facetConfig {
		a.logic = logic
		return a
	}
}

func FacetUsesAndLogic() FacetOption {
	return FacetUsesLogic(model.FacetLogicAnd)
}

func FacetUsesOrLogic() FacetOption {
	return FacetUsesLogic(model.FacetLogicOr)
}

func FacetIsAggregated() FacetOption {
	return func(c facetConfig) facetConfig {
		c.aggregate = true
		return c
	}
}

func FacetHasFilter(filter FacetFilter) FacetOption {
	return func(c facetConfig) facetConfig {
		c.filter = filter
		return c
	}
}

func FacetHasAggregationOption(options ...Option) FacetOption {
	return func(c facetConfig) facetConfig {
		c.aggregationOptions = append(c.aggregationOptions, options...)
		return c
	}
}

func FacetTriggersCte() FacetOption {
	return func(c facetConfig) facetConfig {
		c.triggersCte = true
		return c
	}
}

func (c facetConfig) Key() string {
	return c.key
}

func (c facetConfig) Label() string {
	return c.label
}

func (c facetConfig) Logic() model.FacetLogic {
	return c.logic
}

func (c facetConfig) IsAggregated() bool {
	return c.aggregate
}

func (c facetConfig) AggregationOption(b OptionBuilder) (OptionBuilder, error) {
	return Options(c.aggregationOptions...)(b)
}

func (c facetConfig) Filter() FacetFilter {
	return c.filter
}

func (c facetConfig) TriggersCte() bool {
	return c.triggersCte
}

func (b optionBuilder) createFacetsFilterCriteria() (c Criteria, err error) {
	cs := make([]Criteria, 0, len(b.facets))

	for _, facet := range b.facets {
		cr := facet.Criteria(facet.Filter())

		switch facet.Logic() {
		case model.FacetLogicAnd:
			cs = append(cs, AndCriteria{cr})
		case model.FacetLogicOr:
			if b.currentFacet != facet.Key() {
				cs = append(cs, OrCriteria{cr})
			}
		}
	}

	return AndCriteria{cs}, nil
}

// when aggregating with or logic the current facet's filter should be ignored
func withCurrentFacet(facetKey string) Option {
	return func(b OptionBuilder) (OptionBuilder, error) {
		return b.withCurrentFacet(facetKey), nil
	}
}

func (b optionBuilder) calculateAggregations(ctx context.Context) (Aggregations, error) {
	aggregations := make(Aggregations, len(b.facets))
	var errs []error

	for _, facet := range b.facets {
		if !facet.IsAggregated() {
			continue
		}
		values, valuesErr := facet.Values(facetContext{
			optionBuilder: b,
			ctx:           ctx,
		})
		if valuesErr != nil {
			errs = append(errs, fmt.Errorf("failed to get values for key '%s': %w",
				facet.Key(),
				valuesErr))
			continue
		}
		filter := facet.Filter()
		items := make(AggregationItems, len(values))

		if groupedFacet, ok := facet.(GroupedFacet); ok {
			fld, colErr := groupedFacet.GroupColumn(b)
			if colErr != nil {
				errs = append(errs, fmt.Errorf("failed to get group column for key '%s': %w", facet.Key(), colErr))
				continue
			}
			aggBuilder, aggBuilderErr := Options(
				facet.AggregationOption,
				withCurrentFacet(facet.Key()),
			)(b)
			if aggBuilderErr != nil {
				errs = append(errs, fmt.Errorf("failed to create aggregation option for key '%s': %w", facet.Key(), aggBuilderErr))
				continue
			}
			q := aggBuilder.NewSubQuery(ctx)
			if preErr := aggBuilder.applyPre(q, false); preErr != nil {
				errs = append(errs, fmt.Errorf("failed to apply pre for key '%s': %w", facet.Key(), preErr))
				continue
			}
			colExpr := string(fld.BuildColumn(q.UnderlyingDB().Statement, field.WithTable))
			type groupCount struct {
				Val   *string `gorm:"column:val"`
				Count int64   `gorm:"column:count"`
			}
			var rows []groupCount
			if err := q.UnderlyingDB().
				Select(colExpr + " AS val, count(*) AS count").
				Group(colExpr).
				Find(&rows).Error; err != nil {
				errs = append(errs, fmt.Errorf("failed to execute grouped aggregation for key '%s': %w", facet.Key(), err))
				continue
			}
			groupedCounts := make(map[string]int64, len(rows))
			for _, r := range rows {
				if r.Val == nil {
					groupedCounts["null"] = r.Count
				} else {
					groupedCounts[*r.Val] = r.Count
				}
			}
			for key, count := range groupedCounts {
				if count > 0 || filter.HasKey(key) {
					if label, ok := values[key]; ok {
						items[key] = AggregationItem{
							Label: label,
							Count: uint(count),
						}
					}
				}
			}
			for key := range filter {
				if _, ok := items[key]; !ok {
					if label, ok := values[key]; ok {
						items[key] = AggregationItem{
							Label: label,
							Count: 0,
						}
					}
				}
			}
			aggregations[facet.Key()] = AggregationGroup{
				Label: facet.Label(),
				Logic: facet.Logic(),
				Items: items,
			}
			continue
		}

		for key, label := range values {
			criterias := facet.Criteria(FacetFilter{key: struct{}{}})
			var criteria Criteria
			switch facet.Logic() {
			case model.FacetLogicAnd:
				criteria = AndCriteria{criterias}
			case model.FacetLogicOr:
				criteria = OrCriteria{criterias}
			}
			aggBuilder, aggBuilderErr := Options(
				facet.AggregationOption,
				withCurrentFacet(facet.Key()),
				Where(criteria),
			)(b)
			if aggBuilderErr != nil {
				errs = append(errs, fmt.Errorf("failed to create aggregation option for key '%s': %w", facet.Key(), aggBuilderErr))
				continue
			}
			q := aggBuilder.NewSubQuery(ctx)
			if preErr := aggBuilder.applyPre(q, false); preErr != nil {
				errs = append(errs, fmt.Errorf("failed to apply pre for key '%s': %w", facet.Key(), preErr))
				continue
			}
			var count int64
			if countErr := q.UnderlyingDB().Count(&count).Error; countErr != nil {
				errs = append(errs, fmt.Errorf("failed to get count for key '%s': %w", facet.Key(), countErr))
				continue
			}
			if count > 0 || filter.HasKey(key) {
				items[key] = AggregationItem{
					Label: label,
					Count: uint(count),
				}
			}
		}
		aggregations[facet.Key()] = AggregationGroup{
			Label: facet.Label(),
			Logic: facet.Logic(),
			Items: items,
		}
	}

	return aggregations, errors.Join(errs...)
}

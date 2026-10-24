package query_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) (*dao.Query, *gorm.DB) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	goose.SetBaseFS(migrationssql.FS)
	goose.SetLogger(goose.NopLogger())
	err = goose.SetDialect("sqlite3")
	require.NoError(t, err)
	err = goose.UpContext(context.Background(), sqlDB, ".")
	require.NoError(t, err)

	gormDB, err := gorm.Open(glebarezsqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	return dao.Use(gormDB), gormDB
}

func TestCountCapping(t *testing.T) {
	q, gormDB := setupTestDB(t)

	// 1. Small count test: 5 rows
	for i := 1; i <= 5; i++ {
		err := gormDB.Exec(fmt.Sprintf(
			"INSERT INTO torrent_contents (id, info_hash, content_type, published_at) VALUES ('%d', X'%040x', 'movie', datetime('now'))",
			i, i,
		)).Error
		require.NoError(t, err)
	}

	resSmall, err := query.GenericQuery[model.TorrentContent](
		context.Background(),
		q,
		query.Options(
			query.WithTotalCount(true),
			query.Limit(10),
		),
		model.TableNameTorrentContent,
		func(ctx context.Context, q *dao.Query) query.SubQuery {
			return query.GenericSubQuery[dao.ITorrentContentDo]{
				SubQuery: q.TorrentContent.WithContext(ctx).ReadDB(),
			}
		},
	)
	require.NoError(t, err)
	require.Equal(t, uint(5), resSmall.TotalCount)
	require.False(t, resSmall.TotalCountIsEstimate)

	// 2. Large count test: Insert 1050 rows in batches
	batchSize := 100
	for b := 0; b < 11; b++ {
		var bld strings.Builder
		bld.WriteString("INSERT INTO torrent_contents (id, info_hash, content_type, published_at) VALUES ")
		countInBatch := batchSize
		if b == 10 {
			countInBatch = 50
		}
		for i := 0; i < countInBatch; i++ {
			id := 100 + b*batchSize + i
			if i > 0 {
				bld.WriteString(",")
			}
			fmt.Fprintf(&bld, "('%d', X'%040x', 'movie', datetime('now'))", id, id)
		}
		err := gormDB.Exec(bld.String()).Error
		require.NoError(t, err)
	}

	resCapped, err := query.GenericQuery[model.TorrentContent](
		context.Background(),
		q,
		query.Options(
			query.WithTotalCount(true),
			query.Limit(10),
			query.Where(query.RawCriteria{
				Query: "content_type = ?",
				Args:  []interface{}{"movie"},
			}),
		),
		model.TableNameTorrentContent,
		func(ctx context.Context, q *dao.Query) query.SubQuery {
			return query.GenericSubQuery[dao.ITorrentContentDo]{
				SubQuery: q.TorrentContent.WithContext(ctx).ReadDB(),
			}
		},
	)
	require.NoError(t, err)
	require.Equal(t, uint(query.CountCap), resCapped.TotalCount)
	require.True(t, resCapped.TotalCountIsEstimate)
}

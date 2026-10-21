package search

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/query"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGroupedFacetAggregation(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	goose.SetBaseFS(migrationssql.FS)
	goose.SetLogger(goose.NopLogger())
	err = goose.SetDialect("sqlite3")
	require.NoError(t, err)
	err = goose.UpContext(context.Background(), sqlDB, ".")
	require.NoError(t, err)

	gormDB, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{})
	require.NoError(t, err)

	q := dao.Use(gormDB)

	// Seed test data
	now := time.Now()
	torrents := []*model.TorrentContent{
		{
			ID:          "1",
			InfoHash:    protocol.ID{1},
			ContentType: model.NullContentType{Valid: true, ContentType: model.ContentTypeMovie},
			PublishedAt: now,
		},
		{
			ID:          "2",
			InfoHash:    protocol.ID{2},
			ContentType: model.NullContentType{Valid: true, ContentType: model.ContentTypeMovie},
			PublishedAt: now.Add(-time.Hour),
		},
		{
			ID:          "3",
			InfoHash:    protocol.ID{3},
			ContentType: model.NullContentType{Valid: true, ContentType: model.ContentTypeTvShow},
			PublishedAt: now.Add(-2 * time.Hour),
		},
		{
			ID:          "4",
			InfoHash:    protocol.ID{4},
			ContentType: model.NullContentType{Valid: false},
			PublishedAt: now.Add(-3 * time.Hour),
		},
	}
	err = q.TorrentContent.WithContext(context.Background()).Create(torrents...)
	require.NoError(t, err)

	s := &search{q: q}
	res, err := s.TorrentContent(
		context.Background(),
		query.WithFacet(TorrentContentTypeFacet(query.FacetIsAggregated())),
		query.WithTotalCount(true),
	)
	require.NoError(t, err)
	require.Equal(t, uint(4), res.TotalCount)
	require.Len(t, res.Items, 4)

	agg, ok := res.Aggregations[TorrentContentTypeFacetKey]
	require.True(t, ok)
	require.Equal(t, uint(2), agg.Items["movie"].Count)
	require.Equal(t, uint(1), agg.Items["tv_show"].Count)
	require.Equal(t, uint(1), agg.Items["null"].Count)
}

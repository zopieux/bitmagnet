package dhtcrawler

import (
	"context"
	"database/sql"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupCrawlerTestDB(t *testing.T) (*dao.Query, *gorm.DB) {
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

func TestBackfillMultiFileSummaries(t *testing.T) {
	q, gormDB := setupCrawlerTestDB(t)
	c := &crawler{
		dao:    q,
		logger: zap.NewNop().Sugar(),
	}

	ctx := context.Background()

	// Seed 1 single-file torrent (should stay nil)
	singleHash := protocol.ID([20]byte{1})
	require.NoError(t, gormDB.Exec("INSERT INTO torrents (info_hash, name, size, files_status) VALUES (?, 'single.mkv', 1000, 'single')", singleHash).Error)

	// Seed 2 multi-file torrents missing summaries
	multiHash1 := protocol.ID([20]byte{2})
	require.NoError(t, gormDB.Exec("INSERT INTO torrents (info_hash, name, size, files_status) VALUES (?, 'album', 5000, 'multi')", multiHash1).Error)
	require.NoError(t, gormDB.Exec(`INSERT INTO torrent_files (info_hash, "index", path, size) VALUES (?, 0, 'album/01.flac', 2500)`, multiHash1).Error)
	require.NoError(t, gormDB.Exec(`INSERT INTO torrent_files (info_hash, "index", path, size) VALUES (?, 1, 'album/02.flac', 2500)`, multiHash1).Error)

	multiHash2 := protocol.ID([20]byte{3})
	require.NoError(t, gormDB.Exec("INSERT INTO torrents (info_hash, name, size, files_status) VALUES (?, 'episodes', 8000, 'multi')", multiHash2).Error)
	require.NoError(t, gormDB.Exec(`INSERT INTO torrent_files (info_hash, "index", path, size) VALUES (?, 0, 'episodes/e01.mkv', 4000)`, multiHash2).Error)
	require.NoError(t, gormDB.Exec(`INSERT INTO torrent_files (info_hash, "index", path, size) VALUES (?, 1, 'episodes/e02.mkv', 4000)`, multiHash2).Error)

	// Seed 1 over_threshold torrent (should not be touched by at-start multi backfill)
	overHash := protocol.ID([20]byte{4})
	require.NoError(t, gormDB.Exec("INSERT INTO torrents (info_hash, name, size, files_status) VALUES (?, 'huge', 100000, 'over_threshold')", overHash).Error)

	// Run backfill
	c.backfillMultiFileSummaries(ctx)

	// Verify single stayed NULL
	var singleTorrent model.Torrent
	err := gormDB.Where("info_hash = ?", singleHash).First(&singleTorrent).Error
	require.NoError(t, err)
	require.Nil(t, singleTorrent.FilesSummary)

	// Verify over_threshold stayed NULL
	var overTorrent model.Torrent
	err = gormDB.Where("info_hash = ?", overHash).First(&overTorrent).Error
	require.NoError(t, err)
	require.Nil(t, overTorrent.FilesSummary)

	// Verify multiHash1 and multiHash2 got populated summaries
	var multi1 model.Torrent
	require.NoError(t, gormDB.Where("info_hash = ?", multiHash1).First(&multi1).Error)
	require.NotNil(t, multi1.FilesSummary)
	require.Equal(t, uint64(2), multi1.FilesSummary.TotalFiles)
	require.Equal(t, uint64(5000), multi1.FilesSummary.TotalSize)
	require.Equal(t, "flac", multi1.FilesSummary.TopExts[0].Extension)

	var multi2 model.Torrent
	require.NoError(t, gormDB.Where("info_hash = ?", multiHash2).First(&multi2).Error)
	require.NotNil(t, multi2.FilesSummary)
	require.Equal(t, uint64(2), multi2.FilesSummary.TotalFiles)
	require.Equal(t, uint64(8000), multi2.FilesSummary.TotalSize)
	require.Equal(t, "mkv", multi2.FilesSummary.TopExts[0].Extension)

	// Idempotence test: re-running does not fail and changes nothing
	c.backfillMultiFileSummaries(ctx)
	var multi1Again model.Torrent
	require.NoError(t, gormDB.Where("info_hash = ?", multiHash1).First(&multi1Again).Error)
	require.Equal(t, multi1.FilesSummary.TotalSize, multi1Again.FilesSummary.TotalSize)
}

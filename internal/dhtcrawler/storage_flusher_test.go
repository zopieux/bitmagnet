package dhtcrawler

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/database/cache"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo"
	anacrolixmi "github.com/anacrolix/torrent/metainfo"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	glebarezsqlite "github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFileBackedFlusherTestDB(t *testing.T) (*dao.Query, *sql.DB, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "bitmagnet.db")
	sqlDB, err := sql.Open("sqlite", sqlite.BuildDSN(dbPath))
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	goose.SetBaseFS(migrationssql.FS)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpContext(context.Background(), sqlDB, "."))

	gormDB, err := gorm.Open(glebarezsqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// Exercise the same gorm-cache plugin active in production.
	pluginRes := cache.NewPlugin(cache.PluginParams{
		Config: cache.NewDefaultConfig(),
		Cacher: cache.NewInMemoryCacher(cache.Params{
			Config: cache.NewDefaultConfig(),
			Logger: zap.NewNop().Sugar(),
		}).Cacher,
	})
	require.NoError(t, gormDB.Use(pluginRes.Plugin))

	return dao.Use(gormDB), sqlDB, dbPath
}

func makeTestInfo(name string, files []anacrolixmi.FileInfo) metainfo.Info {
	return metainfo.Info{
		Name:        name,
		PieceLength: 16384,
		Length:      2048,
		Files:       files,
	}
}

func TestStorageFlusherSinglePassAndCheckpoint(t *testing.T) {
	ctx := context.Background()
	q, sqlDB, dbPath := setupFileBackedFlusherTestDB(t)
	walPath := dbPath + "-wal"

	bm := blocking.NewManager(sqlDB, "", time.Minute)
	persistedTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_persisted_total",
		Help: "test",
	}, []string{"entity", "action"})
	var statsPersisted atomic.Int64

	sf := newStorageFlusher(storageFlusherConfig{
		dao:                q,
		sqlDB:              sqlDB,
		walPath:            walPath,
		blockingManager:    bm,
		writeInterval:      time.Minute,
		checkpointInterval: 30 * time.Minute,
		persistedTotal:     persistedTotal,
		statsPersisted:     &statsPersisted,
		logger:             zap.NewNop().Sugar(),
	})

	// Empty flush must be a clean no-op.
	require.NoError(t, sf.Flush(ctx))

	h1 := protocol.ID{0x10}
	h2 := protocol.ID{0x20}
	hBlocked := protocol.ID{0x30}
	hDeleted := protocol.ID{0x40}

	t1, err := createTorrentModel(h1, makeTestInfo("Ubuntu 24.04 Desktop ISO", nil), false, 100)
	require.NoError(t, err)
	t1.Sources[0].Seeders = model.NewNullUint(42)
	t1.Sources[0].Leechers = model.NewNullUint(7)

	t2, err := createTorrentModel(h2, makeTestInfo("Multi Album", []anacrolixmi.FileInfo{
		{Length: 1024, Path: []string{"01.flac"}},
		{Length: 1024, Path: []string{"02.flac"}},
	}), false, 100)
	require.NoError(t, err)

	sf.EnqueueTorrent(pendingTorrent{torrent: t1})
	sf.EnqueueTorrent(pendingTorrent{torrent: t2})
	bm.BlockUnpersisted([]protocol.ID{hBlocked})
	sf.EnqueueRescrape(rescrapeUpdate{
		infoHash: hDeleted,
		seeders:  model.NewNullUint(1),
		leechers: model.NewNullUint(2),
		at:       time.Now(),
	})

	require.NoError(t, sf.Flush(ctx))
	assert.Equal(t, int64(2), statsPersisted.Load())

	var count int
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM queue_jobs").Scan(&count))
	assert.Equal(t, 0, count, "single-pass DHT pipeline must write zero queue_jobs")

	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM torrents").Scan(&count))
	assert.Equal(t, 2, count)

	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM torrent_files").Scan(&count))
	assert.Equal(t, 2, count)

	var tsvVal sql.NullString
	require.NoError(t, sqlDB.QueryRow("SELECT tsv FROM torrent_contents WHERE info_hash = ?", h1[:]).Scan(&tsvVal))
	assert.False(t, tsvVal.Valid && tsvVal.String != "", "dead tsv column must not be populated")

	var ftsHits int
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM torrent_contents_fts WHERE torrent_contents_fts MATCH 'ubuntu'").Scan(&ftsHits))
	assert.Equal(t, 1, ftsHits, "FTS5 trigger must index torrents.name")

	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM blocked_hash_chunks").Scan(&count))
	assert.Equal(t, 1, count, "unpersisted blocked hash must flush in same tx")

	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM bloom_filters").Scan(&count))
	assert.Equal(t, 0, count, "bloom_filters snapshot must not be rewritten on normal flush")

	// Re-flushing h1 (e.g., from probe) with NULL S/L must update summary/name without duplicating torrent_contents or wiping S/L.
	t1Updated, err := createTorrentModel(h1, makeTestInfo("Ubuntu 24.04 Desktop ISO", []anacrolixmi.FileInfo{
		{Length: 2048, Path: []string{"ubuntu.iso"}},
	}), false, 100)
	require.NoError(t, err)
	sf.EnqueueTorrent(pendingTorrent{torrent: t1Updated})
	require.NoError(t, sf.Flush(ctx))

	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM torrent_contents WHERE info_hash = ?", h1[:]).Scan(&count))
	assert.Equal(t, 1, count, "re-flush must not duplicate torrent_contents")

	var seeders uint
	require.NoError(t, sqlDB.QueryRow("SELECT seeders FROM torrents_torrent_sources WHERE info_hash = ? AND source = 'dht'", h1[:]).Scan(&seeders))
	assert.Equal(t, uint(42), seeders, "null new S/L must preserve existing S/L")

	// Rescrape update on h2 must update S/L.
	sf.EnqueueRescrape(rescrapeUpdate{
		infoHash: h2,
		seeders:  model.NewNullUint(99),
		leechers: model.NewNullUint(12),
		at:       time.Now(),
	})
	sf.lastCheckpoint = time.Now().Add(-time.Hour) // Force checkpoint on this flush.
	require.NoError(t, sf.Flush(ctx))

	require.NoError(t, sqlDB.QueryRow("SELECT seeders FROM torrents_torrent_sources WHERE info_hash = ? AND source = 'dht'", h2[:]).Scan(&seeders))
	assert.Equal(t, uint(99), seeders)

	info, statErr := os.Stat(walPath)
	if statErr == nil {
		assert.Equal(t, int64(0), info.Size(), "WAL should be truncated after checkpoint")
	}
}

func TestProbeQueryUsesPartialCoveringIndex(t *testing.T) {
	_, sqlDB, _ := setupFileBackedFlusherTestDB(t)

	rows, err := sqlDB.Query(
		"EXPLAIN QUERY PLAN SELECT info_hash FROM torrents WHERE files_status = 'over_threshold' AND files_summary IS NULL AND info_hash > ? ORDER BY info_hash ASC LIMIT 100",
		make([]byte, 20),
	)
	require.NoError(t, err)
	defer rows.Close()

	var details []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notused, &detail))
		details = append(details, detail)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, details)
	assert.Contains(t, details[0], "USING INDEX idx_torrents_missing_summary")
}

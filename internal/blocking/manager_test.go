package blocking

import (
	"context"
	"database/sql"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/archive"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	goose "github.com/pressly/goose/v3"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE torrents (info_hash BLOB PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE bloom_filters (key TEXT PRIMARY KEY, data BLOB, created_at DATETIME, updated_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE blocked_hash_chunks (id INTEGER PRIMARY KEY, hashes BLOB NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	return db
}

func testManager(t *testing.T, db *sql.DB) Manager {
	t.Helper()

	r := New(Params{
		DB:        lazy.New(func() (*sql.DB, error) { return db, nil }),
	})
	m, err := r.Manager.Get()
	require.NoError(t, err)

	return m
}

func TestBlockingManagerFlush(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	var blockedHash protocol.ID
	copy(blockedHash[:], "blocked-torrent-hash!")

	_, err := db.Exec(`INSERT INTO torrents (info_hash, name) VALUES (?, ?)`, blockedHash[:], "blocked")
	require.NoError(t, err)

	m := testManager(t, db)

	require.NoError(t, m.Block(ctx, []protocol.ID{blockedHash}, true))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM torrents WHERE info_hash = ?`, blockedHash[:]).Scan(&count))
	assert.Equal(t, 0, count, "blocked torrent should be deleted on flush")

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM blocked_hash_chunks`).Scan(&count))
	assert.Equal(t, 1, count, "delta chunk should be persisted on flush")

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bloom_filters WHERE key = ?`, blockedTorrentsBloomFilterKey).Scan(&count))
	assert.Equal(t, 0, count, "bloom_filters snapshot should not be rewritten on flush")

	require.NoError(t, m.Compact(ctx))

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM blocked_hash_chunks`).Scan(&count))
	assert.Equal(t, 0, count, "delta chunks should be cleared after compact")

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bloom_filters WHERE key = ?`, blockedTorrentsBloomFilterKey).Scan(&count))
	assert.Equal(t, 1, count, "bloom filter should be persisted on compact")
}

func TestBlockingManagerFilterLoadsChunksWithoutCompact(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	var blockedHash, freshHash protocol.ID
	copy(blockedHash[:], "blocked-torrent-hash!")
	copy(freshHash[:], "fresh-torrent-hash!!!")

	m := testManager(t, db)
	require.NoError(t, m.Block(ctx, []protocol.ID{blockedHash}, true))

	// A new manager over the same database must pick up uncompacted chunks.
	m2 := testManager(t, db)

	filtered, err := m2.Filter(ctx, []protocol.ID{blockedHash, freshHash})
	require.NoError(t, err)
	assert.Equal(t, []protocol.ID{freshHash}, filtered)

	require.NoError(t, m2.Compact(ctx))

	m3 := testManager(t, db)
	filtered, err = m3.Filter(ctx, []protocol.ID{blockedHash, freshHash})
	require.NoError(t, err)
	assert.Equal(t, []protocol.ID{freshHash}, filtered)
}

func TestBlockingManagerBlockUnpersistedAndPrepareFlushTx(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	var unpersistedHash, persistedHash protocol.ID
	copy(unpersistedHash[:], "unpersisted-hash!!!!!")
	copy(persistedHash[:], "persisted-hash!!!!!!!")

	_, err := db.Exec(`INSERT INTO torrents (info_hash, name) VALUES (?, ?)`, unpersistedHash[:], "keep-if-not-deleted")
	require.NoError(t, err)

	m := testManager(t, db)
	m.BlockUnpersisted([]protocol.ID{unpersistedHash})
	assert.True(t, m.HasPending())

	// Filter skips buffered unpersisted blocks immediately before flush.
	filtered, err := m.Filter(ctx, []protocol.ID{unpersistedHash, persistedHash})
	require.NoError(t, err)
	assert.Equal(t, []protocol.ID{persistedHash}, filtered)

	// Simulate transaction rollback: pending items must remain intact.
	tx1, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = m.PrepareFlushTx(ctx, tx1)
	require.NoError(t, err)
	require.NoError(t, tx1.Rollback())
	assert.True(t, m.HasPending())

	// Commit in second transaction and run onCommit.
	tx2, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	onCommit, err := m.PrepareFlushTx(ctx, tx2)
	require.NoError(t, err)
	require.NoError(t, tx2.Commit())
	onCommit()

	assert.False(t, m.HasPending())

	// BlockUnpersisted must not issue DELETE FROM torrents.
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM torrents WHERE info_hash = ?`, unpersistedHash[:]).Scan(&count))
	assert.Equal(t, 1, count)

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM blocked_hash_chunks`).Scan(&count))
	assert.Equal(t, 1, count)

	filtered, err = m.Filter(ctx, []protocol.ID{unpersistedHash, persistedHash})
	require.NoError(t, err)
	assert.Equal(t, []protocol.ID{persistedHash}, filtered)
}

func TestBlockingManagerArchive(t *testing.T) {
	ctx := context.Background()
	mainDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer mainDB.Close()

	archiveFile := t.TempDir() + "/archive.db"
	archiveDB, err := sql.Open("sqlite", archiveFile)
	require.NoError(t, err)
	defer archiveDB.Close()

	require.NoError(t, archive.Up(ctx, archiveDB))
	mainProvider, err := goose.NewProvider(goose.DialectSQLite3, mainDB, migrationssql.FS)
	require.NoError(t, err)
	_, err = mainProvider.Up(ctx)
	require.NoError(t, err)

	var archivedHash protocol.ID
	copy(archivedHash[:], "archive-torrent-hash!")

	_, err = mainDB.Exec(`INSERT INTO torrents (info_hash, name) VALUES (?, ?)`, archivedHash[:], "archived")
	require.NoError(t, err)

	m := NewManager(mainDB, archiveFile, 1000)

	require.NoError(t, m.Archive(ctx, []protocol.ID{archivedHash}))

	var count int
	require.NoError(t, mainDB.QueryRow(`SELECT COUNT(*) FROM torrents WHERE info_hash = ?`, archivedHash[:]).Scan(&count))
	assert.Equal(t, 0, count, "archived torrent should be deleted from main db")

	filtered, err := m.Filter(ctx, []protocol.ID{archivedHash})
	require.NoError(t, err)
	assert.Empty(t, filtered, "archived torrent should be blocked by filter")

	var archiveCount int
	require.NoError(t, archiveDB.QueryRow(`SELECT COUNT(*) FROM torrents WHERE info_hash = ?`, archivedHash[:]).Scan(&archiveCount))
	assert.Equal(t, 1, archiveCount, "archived torrent should be stored in archive db")
}

func TestBlockingManagerMetrics(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	res := New(Params{
		DB:        lazy.New(func() (*sql.DB, error) { return db, nil }),
	})

	m, err := res.Manager.Get()
	require.NoError(t, err)

	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(res.BlockedTotal))
	require.NoError(t, reg.Register(res.SkippedTotal))

	var blockedHash, freshHash protocol.ID
	copy(blockedHash[:], "blocked-hash-1!!!!!!")
	copy(freshHash[:], "fresh-hash-2!!!!!!!!")

	require.NoError(t, m.Block(ctx, []protocol.ID{blockedHash}, true))

	filtered, err := m.Filter(ctx, []protocol.ID{blockedHash, freshHash})
	require.NoError(t, err)
	assert.Equal(t, []protocol.ID{freshHash}, filtered)

	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	byName := make(map[string]*dto.MetricFamily)
	for _, mf := range metricFamilies {
		byName[*mf.Name] = mf
	}

	require.Contains(t, byName, "bitmagnet_blocking_blocked_total")
	require.Contains(t, byName, "bitmagnet_blocking_skipped_total")

	mfBlocked := byName["bitmagnet_blocking_blocked_total"]
	var blockedCount float64
	for _, metric := range mfBlocked.Metric {
		for _, label := range metric.Label {
			if *label.Name == "action" && *label.Value == "blocked" {
				blockedCount = *metric.Counter.Value
			}
		}
	}
	assert.Equal(t, float64(1), blockedCount)

	mfSkipped := byName["bitmagnet_blocking_skipped_total"]
	assert.Equal(t, float64(1), *mfSkipped.Metric[0].Counter.Value)
}

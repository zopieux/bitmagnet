package blocking

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	_ "github.com/glebarez/go-sqlite"
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

	return db
}

func testManager(t *testing.T, db *sql.DB) Manager {
	t.Helper()

	r := New(Params{
		DB:        lazy.New(func() (*sql.DB, error) { return db, nil }),
		FlushWait: &sync.WaitGroup{},
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

	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bloom_filters WHERE key = ?`, blockedTorrentsBloomFilterKey).Scan(&count))
	assert.Equal(t, 1, count, "bloom filter should be persisted on flush")
}

func TestBlockingManagerFilterLoadsPersistedFilter(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	var blockedHash, freshHash protocol.ID
	copy(blockedHash[:], "blocked-torrent-hash!")
	copy(freshHash[:], "fresh-torrent-hash!!!")

	m := testManager(t, db)
	require.NoError(t, m.Block(ctx, []protocol.ID{blockedHash}, true))

	// a new manager over the same database must pick up the persisted filter
	m2 := testManager(t, db)

	filtered, err := m2.Filter(ctx, []protocol.ID{blockedHash, freshHash})
	require.NoError(t, err)
	assert.Equal(t, []protocol.ID{freshHash}, filtered)
}

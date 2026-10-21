package torrentmetrics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTorrentMetricsRequest(t *testing.T) {
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

	c := client{db: gormDB}

	now := time.Now().UTC().Truncate(time.Minute)
	timeStr := func(t time.Time) string {
		return t.Format("2006-01-02 15:04:05")
	}

	// Insert parent torrents first for foreign keys
	hash1 := protocol.ID{1}
	hash2 := protocol.ID{2}
	hash3 := protocol.ID{3}
	_, err = sqlDB.Exec(`
		INSERT INTO torrents (info_hash, name, size, files_count)
		VALUES (?, 't1', 100, 1), (?, 't2', 200, 1), (?, 't3', 300, 1)
	`, hash1[:], hash2[:], hash3[:])
	require.NoError(t, err)

	// Insert torrent sources
	_, err = sqlDB.Exec(`
		INSERT INTO torrent_sources (key, name) VALUES ('dht', 'DHT'), ('rarbg', 'RARBG')
	`)
	require.NoError(t, err)

	// Insert torrents_torrent_sources: 2 dht, 1 rarbg
	_, err = sqlDB.Exec(`
		INSERT INTO torrents_torrent_sources (source, info_hash, created_at, updated_at)
		VALUES
		('dht', ?, ?, ?),
		('dht', ?, ?, ?),
		('rarbg', ?, ?, ?)
	`,
		hash1[:], timeStr(now.Add(-2*time.Hour)), timeStr(now),
		hash2[:], timeStr(now.Add(-2*time.Hour)), timeStr(now.Add(-5*time.Minute)),
		hash3[:], timeStr(now.Add(-2*time.Hour)), timeStr(now.Add(-10*time.Minute)),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Query last 15 minutes
	buckets, err := c.Request(ctx, Request{
		BucketDuration: "minute",
		StartTime:      now.Add(-15 * time.Minute),
	})
	require.NoError(t, err)
	require.NotEmpty(t, buckets)

	totalCount := uint(0)
	for _, b := range buckets {
		totalCount += b.Count
	}
	require.Equal(t, uint(3), totalCount)

	// Filter by source 'dht'
	dhtBuckets, err := c.Request(ctx, Request{
		BucketDuration: "minute",
		Sources:        []string{"dht"},
		StartTime:      now.Add(-15 * time.Minute),
	})
	require.NoError(t, err)
	dhtCount := uint(0)
	for _, b := range dhtBuckets {
		dhtCount += b.Count
		require.Equal(t, "dht", b.Source)
	}
	require.Equal(t, uint(2), dhtCount)
}

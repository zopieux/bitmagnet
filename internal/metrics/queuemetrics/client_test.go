package queuemetrics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQueueMetricsRequest(t *testing.T) {
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

	// Insert test queue jobs: 1 pending, 2 processed, 1 failed
	_, err = sqlDB.Exec(`
		INSERT INTO queue_jobs (id, fingerprint, queue, status, payload, run_after, ran_at, created_at)
		VALUES
		('job1', 'fp1', 'torrent_process', 'pending', '{}', ?, NULL, ?),
		('job2', 'fp2', 'torrent_process', 'processed', '{}', ?, ?, ?),
		('job3', 'fp3', 'torrent_process', 'processed', '{}', ?, ?, ?),
		('job4', 'fp4', 'torrent_process', 'failed', '{}', ?, ?, ?)
	`,
		timeStr(now), timeStr(now),
		timeStr(now.Add(-time.Hour)), timeStr(now), timeStr(now.Add(-time.Hour)),
		timeStr(now.Add(-time.Hour)), timeStr(now.Add(-10*time.Minute)), timeStr(now.Add(-time.Hour)),
		timeStr(now.Add(-time.Hour)), timeStr(now.Add(-5*time.Minute)), timeStr(now.Add(-time.Hour)),
	)
	require.NoError(t, err)

	ctx := context.Background()

	// Test 1: Query all statuses for last 15 minutes
	buckets, err := c.Request(ctx, Request{
		BucketDuration: "minute",
		StartTime:      now.Add(-15 * time.Minute),
	})
	require.NoError(t, err)
	require.NotEmpty(t, buckets)

	// We expect: job1 (pending at now), job2 (processed at now), job3 (processed at now - 10m), job4 (failed at now - 5m)
	totalCount := uint(0)
	for _, b := range buckets {
		totalCount += b.Count
	}
	require.Equal(t, uint(4), totalCount)

	// Test 2: Filter by status 'processed'
	processedBuckets, err := c.Request(ctx, Request{
		BucketDuration: "minute",
		Statuses:       []model.QueueJobStatus{model.QueueJobStatusProcessed},
		StartTime:      now.Add(-15 * time.Minute),
	})
	require.NoError(t, err)
	processedCount := uint(0)
	for _, b := range processedBuckets {
		processedCount += b.Count
		require.Equal(t, model.QueueJobStatusProcessed, b.Status)
	}
	require.Equal(t, uint(2), processedCount)

	// Test 3: Filter by status 'pending'
	pendingBuckets, err := c.Request(ctx, Request{
		BucketDuration: "minute",
		Statuses:       []model.QueueJobStatus{model.QueueJobStatusPending},
		StartTime:      now.Add(-15 * time.Minute),
	})
	require.NoError(t, err)
	require.Len(t, pendingBuckets, 1)
	require.Equal(t, model.QueueJobStatusPending, pendingBuckets[0].Status)
	require.Equal(t, uint(1), pendingBuckets[0].Count)
}

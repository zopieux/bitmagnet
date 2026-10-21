package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

)

func TestMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	logger := zap.NewNop().Sugar()
	initGoose(logger)

	ctx := context.Background()
	err = goose.UpContext(ctx, db, ".")
	require.NoError(t, err)

	// Verify indexes exist
	var count int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_torrent_contents_published_at', 'idx_torrent_contents_type_published_at', 'idx_torrents_torrent_sources_datetime_updated_at', 'idx_queue_jobs_status_created_at', 'idx_queue_jobs_ran_at')").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 5, count)

	// Test rollback
	err = goose.DownContext(ctx, db, ".")
	require.NoError(t, err)
}

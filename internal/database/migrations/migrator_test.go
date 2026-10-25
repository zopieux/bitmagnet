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
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_torrent_contents_published_at', 'idx_torrent_contents_type_published_at')").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	var hasFilesSummary bool
	rows, err := db.Query("PRAGMA table_info(torrents)")
	require.NoError(t, err)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue any
		err = rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk)
		require.NoError(t, err)
		if name == "files_summary" {
			hasFilesSummary = true
		}
	}
	rows.Close()
	require.True(t, hasFilesSummary)

	// Test rollback
	err = goose.DownContext(ctx, db, ".")
	require.NoError(t, err)
}

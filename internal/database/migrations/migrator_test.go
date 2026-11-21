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

	var blockedChunksCount int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='blocked_hash_chunks'").Scan(&blockedChunksCount)
	require.NoError(t, err)
	require.Equal(t, 1, blockedChunksCount)

	var missingSummaryIdxCount int
	err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_torrents_missing_summary'").Scan(&missingSummaryIdxCount)
	require.NoError(t, err)
	require.Equal(t, 1, missingSummaryIdxCount)

	// Test rollback
	err = goose.DownContext(ctx, db, ".")
	require.NoError(t, err)
}

func TestMigration00006Cleanup(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	initGoose(zap.NewNop().Sugar())

	ctx := context.Background()
	require.NoError(t, goose.UpToContext(ctx, db, ".", 5))

	infoHash := []byte("01234567890123456789")
	_, err = db.Exec(`INSERT INTO torrents (info_hash, name) VALUES (?, ?)`, infoHash, "example torrent")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO torrent_contents (id, info_hash, tsv) VALUES (?, ?, ?)`, "tc-1", infoHash, "legacy")
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO queue_jobs (id, fingerprint, queue, status, payload, run_after) VALUES
		 ('j1', 'f1', 'q', 'processed', '{}', CURRENT_TIMESTAMP),
		 ('j2', 'f2', 'q', 'failed', '{}', CURRENT_TIMESTAMP),
		 ('j3', 'f3', 'q', 'pending', '{}', CURRENT_TIMESTAMP)`,
	)
	require.NoError(t, err)

	require.NoError(t, goose.UpContext(ctx, db, "."))

	var tsv sql.NullString
	require.NoError(t, db.QueryRow(`SELECT tsv FROM torrent_contents WHERE id = 'tc-1'`).Scan(&tsv))
	require.False(t, tsv.Valid, "legacy tsv column should be cleared to NULL")

	var ftsCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM torrent_contents_fts WHERE torrent_contents_fts MATCH 'example'`).Scan(&ftsCount))
	require.Equal(t, 1, ftsCount, "clearing tsv must not duplicate FTS5 postings")

	var triggerCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name='torrent_contents_au'`).Scan(&triggerCount))
	require.Equal(t, 1, triggerCount, "torrent_contents_au trigger must be recreated")

	var remainingID string
	var totalJobs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(id), '') FROM queue_jobs`).Scan(&totalJobs, &remainingID))
	require.Equal(t, 1, totalJobs)
	require.Equal(t, "j3", remainingID)
}

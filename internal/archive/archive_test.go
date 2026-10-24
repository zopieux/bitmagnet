package archive

import (
	"context"
	"database/sql"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	goose "github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveMigrationAndAttach(t *testing.T) {
	ctx := context.Background()
	mainDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer mainDB.Close()

	archiveFile := t.TempDir() + "/archive.db"
	archiveDB, err := sql.Open("sqlite", archiveFile)
	require.NoError(t, err)
	defer archiveDB.Close()

	// 1. Run migrations on mainDB and archiveDB
	require.NoError(t, Up(ctx, archiveDB))
	mainProvider, err := goose.NewProvider(goose.DialectSQLite3, mainDB, migrationssql.FS)
	require.NoError(t, err)
	_, err = mainProvider.Up(ctx)
	require.NoError(t, err)

	// Check that archive has torrents table and doesn't have queue_jobs
	var count int
	require.NoError(t, archiveDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='torrents'`).Scan(&count))
	assert.Equal(t, 1, count)

	require.NoError(t, archiveDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='queue_jobs'`).Scan(&count))
	assert.Equal(t, 0, count)

	require.NoError(t, archiveDB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='torrent_pieces'`).Scan(&count))
	assert.Equal(t, 0, count)

	var h protocol.ID
	copy(h[:], "test-torrent-hash---")

	_, err = mainDB.Exec(`INSERT INTO torrents (info_hash, name, size) VALUES (?, ?, ?)`, h[:], "Ubuntu Linux", 12345)
	require.NoError(t, err)

	_, err = mainDB.Exec(`INSERT INTO torrent_files (info_hash, "index", path, size) VALUES (?, ?, ?, ?)`, h[:], 0, "ubuntu.iso", 12345)
	require.NoError(t, err)

	// Test CopyToArchive using ATTACH
	require.NoError(t, CopyToArchive(ctx, mainDB, archiveFile, []protocol.ID{h}))

	// Verify calling CopyToArchive a second time does not fail with "database archive is already in use"
	require.NoError(t, CopyToArchive(ctx, mainDB, archiveFile, []protocol.ID{h}))

	// Verify in archiveDB directly (test-only verification)
	var name string
	require.NoError(t, archiveDB.QueryRow(`SELECT name FROM torrents WHERE info_hash = ?`, h[:]).Scan(&name))
	assert.Equal(t, "Ubuntu Linux", name)

	var filePath string
	require.NoError(t, archiveDB.QueryRow(`SELECT path FROM torrent_files WHERE info_hash = ?`, h[:]).Scan(&filePath))
	assert.Equal(t, "ubuntu.iso", filePath)
}

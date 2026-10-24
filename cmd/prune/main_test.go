package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/bloom"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"os/exec"
)

func initTestDB(t *testing.T, dbPath string) *sql.DB {
	db, err := sql.Open("sqlite", sqlite.BuildDSN(dbPath))
	require.NoError(t, err)

	goose.SetBaseFS(migrationssql.FS)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpContext(context.Background(), db, "."))

	return db
}

func insertTorrentWithTags(t *testing.T, db *sql.DB, hexHash string, name string, tags []string) {
	id, err := protocol.ParseID(hexHash)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO torrents (info_hash, name, size, files_count) VALUES (?, ?, 1000, 1)`, id[:], name)
	require.NoError(t, err)

	for _, tag := range tags {
		_, err = db.Exec(`INSERT INTO torrent_tags (info_hash, name) VALUES (?, ?)`, id[:], tag)
		require.NoError(t, err)
	}
}

func TestPrune_FlagValidation(t *testing.T) {
	testDir := t.TempDir()
	dbPath := filepath.Join(testDir, "test.db")
	db := initTestDB(t, dbPath)
	_ = db.Close()

	// Missing both --delete and --archive
	cmd := exec.Command("go", "run", ".", "--tag=foo", "--dsn="+dbPath)
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "must specify exactly one of --delete or --archive")

	// Both --delete and --archive
	cmd = exec.Command("go", "run", ".", "--tag=foo", "--delete", "--archive", "--dsn="+dbPath)
	out, err = cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "must specify exactly one of --delete or --archive")

	// Missing --tag
	cmd = exec.Command("go", "run", ".", "--delete", "--dsn="+dbPath)
	out, err = cmd.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(out), "at least one non-empty --tag must be specified")
}

func TestPrune_AndFiltering(t *testing.T) {
	testDir := t.TempDir()
	dbPath := filepath.Join(testDir, "test.db")
	archivePath := filepath.Join(testDir, "archive.db")
	db := initTestDB(t, dbPath)

	h1 := "1111111111111111111111111111111111111111" // only "ok"
	h2 := "2222222222222222222222222222222222222222" // only "nsfw"
	h3 := "3333333333333333333333333333333333333333" // both "ok" AND "nsfw"
	h4 := "4444444444444444444444444444444444444444" // both "ok" AND "nsfw" (plus "other")
	h5 := "5555555555555555555555555555555555555555" // neither

	insertTorrentWithTags(t, db, h1, "Torrent 1", []string{"ok"})
	insertTorrentWithTags(t, db, h2, "Torrent 2", []string{"nsfw"})
	insertTorrentWithTags(t, db, h3, "Torrent 3", []string{"ok", "nsfw"})
	insertTorrentWithTags(t, db, h4, "Torrent 4", []string{"ok", "nsfw", "other"})
	insertTorrentWithTags(t, db, h5, "Torrent 5", []string{"clean"})
	_ = db.Close()

	// 1. Delete torrents matching BOTH "ok" AND "nsfw"
	cmd := exec.Command("go", "run", ".", "--tag", "ok", "--tag", "nsfw", "--delete", "--batch-size=1", "--dsn="+dbPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "prune delete failed: %s", string(out))
	require.Contains(t, string(out), "Deleting: done, 2 torrents")

	verifyDB, err := sql.Open("sqlite", sqlite.BuildDSN(dbPath))
	require.NoError(t, err)

	// h3 and h4 must be deleted from main DB
	for _, h := range []string{h3, h4} {
		var count int
		require.NoError(t, verifyDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE hex(info_hash) = ?", h).Scan(&count))
		require.Equal(t, 0, count, "hash %s should have been deleted", h)
	}

	// h1, h2, h5 must still be in main DB
	for _, h := range []string{h1, h2, h5} {
		var count int
		require.NoError(t, verifyDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE hex(info_hash) = ?", h).Scan(&count))
		require.Equal(t, 1, count, "hash %s should still exist", h)
	}

	// Verify bloom filter has h3 and h4 blocked, but not h1, h2, h5
	var bfData []byte
	require.NoError(t, verifyDB.QueryRow("SELECT data FROM bloom_filters WHERE key = 'blocked_torrents'").Scan(&bfData))
	bf := bloom.NewDefaultStableBloomFilter()
	require.NoError(t, bf.GobDecode(bfData))

	for _, h := range []string{h3, h4} {
		id, _ := protocol.ParseID(h)
		require.True(t, bf.Test(id[:]), "deleted hash %s should be in bloom filter", h)
	}
	for _, h := range []string{h1, h2, h5} {
		id, _ := protocol.ParseID(h)
		require.False(t, bf.Test(id[:]), "kept hash %s should NOT be in bloom filter", h)
	}
	_ = verifyDB.Close()

	// 2. Archive torrents matching tag "ok" (only h1 matches now since h3, h4 were deleted)
	cmd = exec.Command("go", "run", ".", "--tag", "ok", "--archive", "--dsn="+dbPath)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "prune archive failed: %s", string(out))
	require.Contains(t, string(out), "Archiving: done, 1 torrents")

	verifyDB, err = sql.Open("sqlite", sqlite.BuildDSN(dbPath))
	require.NoError(t, err)
	var count int
	require.NoError(t, verifyDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE hex(info_hash) = ?", h1).Scan(&count))
	require.Equal(t, 0, count, "archived hash %s should be removed from main DB", h1)
	_ = verifyDB.Close()

	// Check archive.db
	adb, err := sql.Open("sqlite", sqlite.BuildDSN(archivePath))
	require.NoError(t, err)
	defer adb.Close()

	require.NoError(t, adb.QueryRow("SELECT COUNT(*) FROM torrents WHERE hex(info_hash) = ?", h1).Scan(&count))
	require.Equal(t, 1, count, "archived hash %s should be in archive.db", h1)

	// Verify h2 and h5 are not in archive.db
	for _, h := range []string{h2, h5} {
		require.NoError(t, adb.QueryRow("SELECT COUNT(*) FROM torrents WHERE hex(info_hash) = ?", h).Scan(&count))
		require.Equal(t, 0, count, "hash %s should not be in archive.db", h)
	}
}

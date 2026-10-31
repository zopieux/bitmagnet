package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/archive"
	"github.com/bitmagnet-io/bitmagnet/internal/bloom"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func initMainDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", sqlite.BuildDSN(dbPath))
	require.NoError(t, err)

	goose.SetBaseFS(migrationssql.FS)
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("sqlite3"))
	require.NoError(t, goose.UpContext(context.Background(), db, "."))

	return db
}

func insertTorrent(t *testing.T, db *sql.DB, hexHash, name string, files []string) {
	t.Helper()

	id, err := protocol.ParseID(hexHash)
	require.NoError(t, err)

	_, err = db.Exec(
		`INSERT INTO torrents (info_hash, name, size, files_count) VALUES (?, ?, 1000, ?)`,
		id[:], name, len(files),
	)
	require.NoError(t, err)

	for i, path := range files {
		_, err = db.Exec(
			`INSERT INTO torrent_files (info_hash, "index", path, extension, size) VALUES (?, ?, ?, ?, 1000)`,
			id[:], i, path, filepath.Ext(path),
		)
		require.NoError(t, err)
	}
}

func TestBackfill(t *testing.T) {
	testDir := t.TempDir()
	dbPath := filepath.Join(testDir, "bitmagnet.db")
	archivePath := filepath.Join(testDir, "archive.db")

	db := initMainDB(t, dbPath)

	deleted := []string{
		"1111111111111111111111111111111111111111",
		"4444444444444444444444444444444444444444",
		"6666666666666666666666666666666666666666",
		"7777777777777777777777777777777777777777",
		"8888888888888888888888888888888888888888",
		"9999999999999999999999999999999999999999",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	archived := []string{
		"3333333333333333333333333333333333333333",
		"cccccccccccccccccccccccccccccccccccccccc",
	}
	kept := []string{
		"2222222222222222222222222222222222222222",
		"5555555555555555555555555555555555555555",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}

	insertTorrent(t, db, deleted[0], "Кошка и самурай", nil)
	insertTorrent(t, db, kept[0], "example.ru release", nil)
	insertTorrent(t, db, archived[0], "Brazzers - Some Scene", nil)
	insertTorrent(t, db, deleted[1], "pthc collection", nil)
	insertTorrent(t, db, kept[1], "The Big Short 2015", nil)
	insertTorrent(t, db, deleted[2], "Clean name", []string{"Кошка.mkv"})
	insertTorrent(t, db, deleted[3], "한국 영화", nil)
	insertTorrent(t, db, deleted[4], "中文电影", nil)
	insertTorrent(t, db, deleted[5], "日本語のタイトル", nil)
	insertTorrent(t, db, deleted[6], "ภาพยนตร์ไทย", nil)
	// A porn-matching name with only audio files bails out of the porn check.
	insertTorrent(t, db, kept[2], "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44]", []string{"California.flac"})
	// The same name with a video file does not bail out.
	insertTorrent(t, db, archived[1], "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44]", []string{"California.mkv"})
	_ = db.Close()

	configPath := filepath.Join(testDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`classify:
  banned_action: delete
  porn_action: archive
  cyrillic_action: delete
  korean_action: delete
  chinese_action: delete
  japanese_action: delete
  thai_action: delete
`), 0644))

	cmd := exec.Command(
		"go", "run", ".",
		"-config", configPath,
		"-workers", "4",
		"-batch-size", "3",
		"-flush-size", "100",
		testDir,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "backfill failed: %s", string(out))
	require.Contains(t, string(out), "deleted 7, archived 2, kept 3")

	verifyDB, err := sql.Open("sqlite", sqlite.BuildDSN(dbPath))
	require.NoError(t, err)
	defer verifyDB.Close()

	for _, hash := range append(deleted, archived...) {
		var count int
		require.NoError(t, verifyDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE lower(hex(info_hash)) = ?", hash).Scan(&count))
		require.Equal(t, 0, count, "hash %s should have been removed", hash)
	}

	for _, hash := range kept {
		var count int
		require.NoError(t, verifyDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE lower(hex(info_hash)) = ?", hash).Scan(&count))
		require.Equal(t, 1, count, "hash %s should still exist", hash)
	}

	var bloomData []byte
	require.NoError(t, verifyDB.QueryRow("SELECT data FROM bloom_filters WHERE key = 'blocked_torrents'").Scan(&bloomData))
	bf := bloom.NewDefaultStableBloomFilter()
	require.NoError(t, bf.GobDecode(bloomData))

	for _, hash := range append(deleted, archived...) {
		id, err := protocol.ParseID(hash)
		require.NoError(t, err)
		require.True(t, bf.Test(id[:]), "hash %s should be in bloom filter", hash)
	}

	for _, hash := range kept {
		id, err := protocol.ParseID(hash)
		require.NoError(t, err)
		require.False(t, bf.Test(id[:]), "hash %s should not be in bloom filter", hash)
	}

	archiveDB, err := sql.Open("sqlite", sqlite.BuildDSN(archivePath))
	require.NoError(t, err)
	defer archiveDB.Close()

	require.NoError(t, archive.Up(context.Background(), archiveDB))

	for _, hash := range archived {
		var count int
		require.NoError(t, archiveDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE lower(hex(info_hash)) = ?", hash).Scan(&count))
		require.Equal(t, 1, count, "archived hash %s should be in archive.db", hash)
	}

	for _, hash := range kept {
		var count int
		require.NoError(t, archiveDB.QueryRow("SELECT COUNT(*) FROM torrents WHERE lower(hex(info_hash)) = ?", hash).Scan(&count))
		require.Equal(t, 0, count, "kept hash %s should not be in archive.db", hash)
	}
}

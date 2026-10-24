package archive

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	goose "github.com/pressly/goose/v3"
)

const batchSize = 500

var archiveTables = []string{
	"torrents",
	"torrent_files",
	"torrent_contents",
	"torrent_tags",
	"torrents_torrent_sources",
	"torrent_hints",
}

type memFile struct {
	*bytes.Reader
	name string
	size int64
}

func (m *memFile) Close() error {
	return nil
}

func (m *memFile) Stat() (fs.FileInfo, error) {
	return memFileInfo{name: m.name, size: m.size}, nil
}

type memFileInfo struct {
	name string
	size int64
}

func (m memFileInfo) Name() string       { return m.name }
func (m memFileInfo) Size() int64        { return m.size }
func (m memFileInfo) Mode() fs.FileMode  { return 0644 }
func (m memFileInfo) ModTime() time.Time { return time.Time{} }
func (m memFileInfo) IsDir() bool        { return false }
func (m memFileInfo) Sys() any           { return nil }

type archiveFS struct {
	base fs.FS
}

func (a archiveFS) Open(name string) (fs.File, error) {
	file, err := a.base.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.IsDir() {
		return file, nil
	}

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	filtered := filterMigrationSQL(string(content))
	return &memFile{
		Reader: bytes.NewReader(filtered),
		name:   stat.Name(),
		size:   int64(len(filtered)),
	}, nil
}

func filterMigrationSQL(sqlText string) []byte {
	upSection := sqlText
	if upIdx := strings.Index(sqlText, "-- +goose Up"); upIdx != -1 {
		upSection = sqlText[upIdx+len("-- +goose Up"):]
	}
	if downIdx := strings.Index(upSection, "-- +goose Down"); downIdx != -1 {
		upSection = upSection[:downIdx]
	}

	var out strings.Builder
	out.WriteString("-- +goose Up\n")

	var current strings.Builder
	hasArchive := false

	for _, line := range strings.Split(upSection, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- +archive") {
			hasArchive = true
		}
		current.WriteString(line)
		current.WriteString("\n")

		if strings.HasSuffix(trimmed, ";") && !strings.HasPrefix(trimmed, "--") {
			if hasArchive {
				out.WriteString(current.String())
				out.WriteString("\n")
			}
			current.Reset()
			hasArchive = false
		}
	}

	out.WriteString("-- +goose Down\n")
	return []byte(out.String())
}

func Up(ctx context.Context, db *sql.DB) error {
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		archiveFS{base: migrationssql.FS},
	)
	if err != nil {
		return fmt.Errorf("failed to create goose provider: %w", err)
	}
	_, err = provider.Up(ctx)
	return err
}

func CopyToArchive(ctx context.Context, mainDB *sql.DB, archivePath string, hashes []protocol.ID) error {
	if len(hashes) == 0 {
		return nil
	}

	escapedPath := strings.ReplaceAll(archivePath, "'", "''")
	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS archive", escapedPath)

	if _, err := mainDB.ExecContext(ctx, attachQuery); err != nil {
		return fmt.Errorf("failed to attach archive db: %w", err)
	}
	defer func() {
		_, _ = mainDB.ExecContext(ctx, "DETACH DATABASE archive")
	}()

	for batch := range slices.Chunk(hashes, batchSize) {
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = strings.TrimSuffix(placeholders, ",")

		args := make([]any, len(batch))
		for i, h := range batch {
			args[i] = h[:]
		}

		for _, tbl := range archiveTables {
			query := fmt.Sprintf("INSERT OR REPLACE INTO archive.%s SELECT * FROM main.%s WHERE info_hash IN (%s)", tbl, tbl, placeholders)
			if _, err := mainDB.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("failed to copy %s to archive: %w", tbl, err)
			}
		}

		sourcesQuery := fmt.Sprintf("INSERT OR IGNORE INTO archive.torrent_sources SELECT * FROM main.torrent_sources WHERE key IN (SELECT source FROM main.torrents_torrent_sources WHERE info_hash IN (%s))", placeholders)
		if _, err := mainDB.ExecContext(ctx, sourcesQuery, args...); err != nil {
			return fmt.Errorf("failed to copy torrent_sources to archive: %w", err)
		}
	}

	return nil
}

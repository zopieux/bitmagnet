package blocking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/archive"
	"github.com/bitmagnet-io/bitmagnet/internal/bloom"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

type Manager interface {
	Filter(ctx context.Context, hashes []protocol.ID) ([]protocol.ID, error)
	Block(ctx context.Context, hashes []protocol.ID, flush bool) error
	Archive(ctx context.Context, hashes []protocol.ID) error
	Flush(ctx context.Context) error
}

type manager struct {
	mutex         sync.Mutex
	db            *sql.DB
	archivePath   string
	buffer        map[protocol.ID]struct{}
	filter        *bloom.StableBloomFilter
	maxBufferSize int
	lastFlushedAt time.Time
	maxFlushWait  time.Duration
}

func NewManager(db *sql.DB, archivePath string, maxFlushWait time.Duration) Manager {
	return &manager{
		db:            db,
		archivePath:   archivePath,
		buffer:        make(map[protocol.ID]struct{}, 1000),
		maxBufferSize: 1000,
		maxFlushWait:  maxFlushWait,
	}
}

func (m *manager) Filter(ctx context.Context, hashes []protocol.ID) ([]protocol.ID, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.filter == nil {
		if err := m.loadFilter(ctx); err != nil {
			return nil, err
		}
	}

	if m.shouldFlush() {
		if flushErr := m.flush(ctx); flushErr != nil {
			return nil, flushErr
		}
	}

	filtered := make([]protocol.ID, 0, len(hashes))

	for _, hash := range hashes {
		if _, ok := m.buffer[hash]; ok {
			continue
		}

		if m.filter.Test(hash[:]) {
			continue
		}

		filtered = append(filtered, hash)
	}

	return filtered, nil
}

func (m *manager) Block(ctx context.Context, hashes []protocol.ID, flush bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, hash := range hashes {
		m.buffer[hash] = struct{}{}
	}

	if flush || m.shouldFlush() {
		if flushErr := m.flush(ctx); flushErr != nil {
			return flushErr
		}
	}

	return nil
}

func (m *manager) Archive(ctx context.Context, hashes []protocol.ID) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.archivePath != "" {
		if err := archive.CopyToArchive(ctx, m.db, m.archivePath, hashes); err != nil {
			return err
		}
	}

	for _, hash := range hashes {
		m.buffer[hash] = struct{}{}
	}

	return m.flush(ctx)
}

func (m *manager) Flush(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if len(m.buffer) == 0 {
		return nil
	}

	return m.flush(ctx)
}

const blockedTorrentsBloomFilterKey = "blocked_torrents"

// deleteBatchSize keeps the number of placeholders per statement well below
// the SQLite variable limit.
const deleteBatchSize = 500

func (m *manager) loadFilter(ctx context.Context) error {
	var data []byte

	err := m.db.QueryRowContext(ctx,
		"SELECT data FROM bloom_filters WHERE key = ?",
		blockedTorrentsBloomFilterKey,
	).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read bloom filter: %w", err)
	}

	bf := bloom.NewDefaultStableBloomFilter()
	if len(data) > 0 {
		if err := bf.GobDecode(data); err != nil {
			return fmt.Errorf("failed to decode bloom filter: %w", err)
		}
	}

	m.filter = bf
	m.lastFlushedAt = time.Now()
	return nil
}

func (m *manager) flush(ctx context.Context) error {
	if len(m.buffer) == 0 {
		return nil
	}

	if m.filter == nil {
		if err := m.loadFilter(ctx); err != nil {
			return err
		}
	}

	hashes := slices.Collect(maps.Keys(m.buffer))

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	for batch := range slices.Chunk(hashes, deleteBatchSize) {
		placeholders := strings.Repeat("?,", len(batch))
		placeholders = strings.TrimSuffix(placeholders, ",")

		args := make([]any, len(batch))
		for i, h := range batch {
			args[i] = h[:]
		}

		if _, err := tx.ExecContext(ctx,
			"DELETE FROM torrents WHERE info_hash IN ("+placeholders+")",
			args...,
		); err != nil {
			return fmt.Errorf("failed to delete from torrents table: %w", err)
		}
	}

	for _, hash := range hashes {
		m.filter.Add(hash[:])
	}

	encoded, err := m.filter.GobEncode()
	if err != nil {
		return fmt.Errorf("failed to encode bloom filter: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO bloom_filters (key, data, created_at, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET data = excluded.data, updated_at = CURRENT_TIMESTAMP`,
		blockedTorrentsBloomFilterKey,
		encoded,
	); err != nil {
		return fmt.Errorf("failed to store bloom filter: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	m.buffer = make(map[protocol.ID]struct{})
	m.lastFlushedAt = time.Now()

	return nil
}

func (m *manager) shouldFlush() bool {
	return len(m.buffer) > 0 && (len(m.buffer) >= m.maxBufferSize || time.Since(m.lastFlushedAt) >= m.maxFlushWait)
}

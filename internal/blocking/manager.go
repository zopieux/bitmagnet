package blocking

import (
	"bytes"
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
	"github.com/prometheus/client_golang/prometheus"
)

type SQLExecQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Manager interface {
	Filter(ctx context.Context, hashes []protocol.ID) ([]protocol.ID, error)
	Block(ctx context.Context, hashes []protocol.ID, flush bool) error
	BlockUnpersisted(hashes []protocol.ID)
	Archive(ctx context.Context, hashes []protocol.ID) error
	HasPending() bool
	Flush(ctx context.Context) error
	PrepareFlushTx(ctx context.Context, q SQLExecQuerier) (func(), error)
	Compact(ctx context.Context) error
	CompactIfNeeded(ctx context.Context, maxUncompacted int, maxAge time.Duration) (bool, error)
}

type Option func(*manager)

func WithMetrics(blockedTotal *prometheus.CounterVec, skippedTotal prometheus.Counter) Option {
	return func(m *manager) {
		m.blockedTotal = blockedTotal
		m.skippedTotal = skippedTotal
	}
}

type manager struct {
	mutex                     sync.Mutex
	loadMu                    sync.Mutex
	db                        *sql.DB
	archivePath               string
	pendingBlock              map[protocol.ID]struct{}
	pendingDelete             map[protocol.ID]struct{}
	committedBeforeFilterLoad []protocol.ID
	filter                    *bloom.StableBloomFilter
	dirtySnapshot             bool
	uncompactedCount          int
	lastCompactedAt           time.Time
	maxBufferSize             int
	lastFlushedAt             time.Time
	maxFlushWait              time.Duration
	blockedTotal              *prometheus.CounterVec
	skippedTotal              prometheus.Counter
}

func NewManager(db *sql.DB, archivePath string, maxFlushWait time.Duration, options ...Option) Manager {
	now := time.Now()
	m := &manager{
		db:              db,
		archivePath:     archivePath,
		pendingBlock:    make(map[protocol.ID]struct{}, 1000),
		pendingDelete:   make(map[protocol.ID]struct{}, 1000),
		maxBufferSize:   10000,
		maxFlushWait:    maxFlushWait,
		lastFlushedAt:   now,
		lastCompactedAt: now,
	}
	for _, opt := range options {
		opt(m)
	}
	return m
}

func (m *manager) ensureFilterLoaded(ctx context.Context) error {
	m.mutex.Lock()
	loaded := m.filter != nil
	m.mutex.Unlock()
	if loaded {
		return nil
	}

	m.loadMu.Lock()
	defer m.loadMu.Unlock()

	m.mutex.Lock()
	loaded = m.filter != nil
	m.mutex.Unlock()
	if loaded {
		return nil
	}

	bf, dirty, chunkHashes, err := readFilterFromDB(ctx, m.db)
	if err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.filter == nil {
		for _, h := range m.committedBeforeFilterLoad {
			bf.Add(h[:])
		}
		if len(m.committedBeforeFilterLoad) > 0 {
			dirty = true
			chunkHashes += len(m.committedBeforeFilterLoad)
		}
		m.committedBeforeFilterLoad = nil
		m.filter = bf
		m.dirtySnapshot = dirty
		m.uncompactedCount = chunkHashes
	}
	return nil
}

func (m *manager) Filter(ctx context.Context, hashes []protocol.ID) ([]protocol.ID, error) {
	if err := m.ensureFilterLoaded(ctx); err != nil {
		return nil, err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	filtered := make([]protocol.ID, 0, len(hashes))

	for _, hash := range hashes {
		if _, ok := m.pendingBlock[hash]; ok {
			continue
		}

		if m.filter.Test(hash[:]) {
			continue
		}

		filtered = append(filtered, hash)
	}

	if m.skippedTotal != nil {
		if skipped := len(hashes) - len(filtered); skipped > 0 {
			m.skippedTotal.Add(float64(skipped))
		}
	}

	return filtered, nil
}

func (m *manager) BlockUnpersisted(hashes []protocol.ID) {
	if len(hashes) == 0 {
		return
	}

	m.mutex.Lock()
	for _, hash := range hashes {
		m.pendingBlock[hash] = struct{}{}
	}
	m.mutex.Unlock()

	if m.blockedTotal != nil {
		m.blockedTotal.WithLabelValues("blocked").Add(float64(len(hashes)))
	}
}

func (m *manager) Block(ctx context.Context, hashes []protocol.ID, flush bool) error {
	m.mutex.Lock()
	for _, hash := range hashes {
		m.pendingBlock[hash] = struct{}{}
		m.pendingDelete[hash] = struct{}{}
	}
	needFlush := flush || (len(m.pendingBlock) >= m.maxBufferSize)
	m.mutex.Unlock()

	if m.blockedTotal != nil && len(hashes) > 0 {
		m.blockedTotal.WithLabelValues("blocked").Add(float64(len(hashes)))
	}

	if needFlush {
		return m.Flush(ctx)
	}

	return nil
}

func (m *manager) Archive(ctx context.Context, hashes []protocol.ID) error {
	if m.archivePath != "" {
		if err := archive.CopyToArchive(ctx, m.db, m.archivePath, hashes); err != nil {
			return err
		}
	}

	m.mutex.Lock()
	for _, hash := range hashes {
		m.pendingBlock[hash] = struct{}{}
		m.pendingDelete[hash] = struct{}{}
	}
	m.mutex.Unlock()

	if m.blockedTotal != nil && len(hashes) > 0 {
		m.blockedTotal.WithLabelValues("archived").Add(float64(len(hashes)))
	}

	return m.Flush(ctx)
}

func (m *manager) HasPending() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return len(m.pendingBlock) > 0 || len(m.pendingDelete) > 0
}

func (m *manager) PrepareFlushTx(ctx context.Context, q SQLExecQuerier) (func(), error) {
	m.mutex.Lock()
	deletes := slices.Collect(maps.Keys(m.pendingDelete))
	blocks := slices.Collect(maps.Keys(m.pendingBlock))
	m.mutex.Unlock()

	if len(deletes) == 0 && len(blocks) == 0 {
		return func() {}, nil
	}

	if len(deletes) > 0 {
		slices.SortFunc(deletes, func(a, b protocol.ID) int {
			return bytes.Compare(a[:], b[:])
		})
		for batch := range slices.Chunk(deletes, deleteBatchSize) {
			placeholders := strings.Repeat("?,", len(batch))
			placeholders = strings.TrimSuffix(placeholders, ",")

			args := make([]any, len(batch))
			for i, h := range batch {
				args[i] = h[:]
			}

			if _, err := q.ExecContext(ctx,
				"DELETE FROM torrents WHERE info_hash IN ("+placeholders+")",
				args...,
			); err != nil {
				return nil, fmt.Errorf("failed to delete from torrents table: %w", err)
			}
		}
	}

	if len(blocks) > 0 {
		buf := make([]byte, len(blocks)*len(protocol.ID{}))
		for i, h := range blocks {
			copy(buf[i*len(h):(i+1)*len(h)], h[:])
		}
		if _, err := q.ExecContext(ctx,
			"INSERT INTO blocked_hash_chunks (hashes) VALUES (?)",
			buf,
		); err != nil {
			return nil, fmt.Errorf("failed to insert blocked_hash_chunks: %w", err)
		}
	}

	onCommit := func() {
		m.mutex.Lock()
		defer m.mutex.Unlock()

		if m.filter != nil {
			for _, h := range blocks {
				m.filter.Add(h[:])
			}
		} else if len(blocks) > 0 {
			m.committedBeforeFilterLoad = append(m.committedBeforeFilterLoad, blocks...)
		}

		for _, h := range blocks {
			delete(m.pendingBlock, h)
		}
		for _, h := range deletes {
			delete(m.pendingDelete, h)
		}

		if len(blocks) > 0 {
			m.dirtySnapshot = true
			m.uncompactedCount += len(blocks)
		}
		m.lastFlushedAt = time.Now()
	}

	return onCommit, nil
}

func (m *manager) Flush(ctx context.Context) error {
	if !m.HasPending() {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	onCommit, err := m.PrepareFlushTx(ctx, tx)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	onCommit()
	return nil
}

func (m *manager) Compact(ctx context.Context) error {
	if err := m.Flush(ctx); err != nil {
		return err
	}

	if err := m.ensureFilterLoaded(ctx); err != nil {
		return err
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin compaction transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var maxChunkID int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM blocked_hash_chunks").Scan(&maxChunkID); err != nil {
		if !isNoSuchTableErr(err) {
			return fmt.Errorf("failed to query blocked_hash_chunks max id: %w", err)
		}
		maxChunkID = 0
	}

	m.mutex.Lock()
	if !m.dirtySnapshot && maxChunkID == 0 {
		m.mutex.Unlock()
		return nil
	}
	encoded, err := m.filter.GobEncode()
	m.mutex.Unlock()
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

	if maxChunkID > 0 {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM blocked_hash_chunks WHERE id <= ?",
			maxChunkID,
		); err != nil {
			return fmt.Errorf("failed to delete compacted blocked_hash_chunks: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit compaction transaction: %w", err)
	}

	m.mutex.Lock()
	m.dirtySnapshot = false
	m.uncompactedCount = 0
	m.lastCompactedAt = time.Now()
	m.mutex.Unlock()

	return nil
}

func (m *manager) CompactIfNeeded(ctx context.Context, maxUncompacted int, maxAge time.Duration) (bool, error) {
	m.mutex.Lock()
	shouldCompact := m.dirtySnapshot &&
		((maxUncompacted > 0 && m.uncompactedCount >= maxUncompacted) ||
			(maxAge > 0 && time.Since(m.lastCompactedAt) >= maxAge))
	m.mutex.Unlock()

	if !shouldCompact {
		return false, nil
	}

	if err := m.Compact(ctx); err != nil {
		return false, err
	}
	return true, nil
}

const blockedTorrentsBloomFilterKey = "blocked_torrents"

// deleteBatchSize keeps the number of placeholders per statement well below
// the SQLite variable limit.
const deleteBatchSize = 500

func isNoSuchTableErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func readFilterFromDB(ctx context.Context, q SQLExecQuerier) (*bloom.StableBloomFilter, bool, int, error) {
	var data []byte

	err := q.QueryRowContext(ctx,
		"SELECT data FROM bloom_filters WHERE key = ?",
		blockedTorrentsBloomFilterKey,
	).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, 0, fmt.Errorf("failed to read bloom filter: %w", err)
	}

	bf := bloom.NewDefaultStableBloomFilter()
	if len(data) > 0 {
		if err := bf.GobDecode(data); err != nil {
			return nil, false, 0, fmt.Errorf("failed to decode bloom filter: %w", err)
		}
	}

	rows, err := q.QueryContext(ctx, "SELECT hashes FROM blocked_hash_chunks ORDER BY id ASC")
	if err != nil {
		if isNoSuchTableErr(err) {
			return bf, false, 0, nil
		}
		return nil, false, 0, fmt.Errorf("failed to read blocked_hash_chunks: %w", err)
	}
	defer rows.Close()

	dirty := false
	chunkHashes := 0
	hashLen := len(protocol.ID{})
	for rows.Next() {
		var chunk []byte
		if err := rows.Scan(&chunk); err != nil {
			return nil, false, 0, fmt.Errorf("failed to scan blocked_hash_chunks: %w", err)
		}
		for i := 0; i+hashLen <= len(chunk); i += hashLen {
			bf.Add(chunk[i : i+hashLen])
			dirty = true
			chunkHashes++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false, 0, fmt.Errorf("failed iterating blocked_hash_chunks: %w", err)
	}

	return bf, dirty, chunkHashes, nil
}

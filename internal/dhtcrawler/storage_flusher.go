package dhtcrawler

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/classify"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/processor"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultFlusherMaxBatchSize = 2500
	defaultFlusherMaxWALBytes  = 256 * 1024 * 1024
)

type pendingTorrent struct {
	nodeHasPeersForHash
	torrent      model.Torrent
	archiveAfter bool
}

type rescrapeUpdate struct {
	infoHash protocol.ID
	seeders  model.NullUint
	leechers model.NullUint
	at       time.Time
}

type storageFlusher struct {
	dao                *dao.Query
	sqlDB              *sql.DB
	walPath            string
	blockingManager    blocking.Manager
	savePieces         bool
	writeInterval      time.Duration
	checkpointInterval time.Duration
	maxBatchSize       int
	maxWALBytes        int64
	persistedTotal     *prometheus.CounterVec
	statsPersisted     *atomic.Int64
	logger             *zap.SugaredLogger

	mu               sync.Mutex
	pendingTorrents  map[protocol.ID]pendingTorrent
	pendingRescrapes map[protocol.ID]rescrapeUpdate
	triggerCh        chan struct{}

	flushMu        sync.Mutex
	lastCheckpoint time.Time
}

type storageFlusherConfig struct {
	dao                *dao.Query
	sqlDB              *sql.DB
	walPath            string
	blockingManager    blocking.Manager
	savePieces         bool
	writeInterval      time.Duration
	checkpointInterval time.Duration
	persistedTotal     *prometheus.CounterVec
	statsPersisted     *atomic.Int64
	logger             *zap.SugaredLogger
}

func newStorageFlusher(cfg storageFlusherConfig) *storageFlusher {
	writeInterval := cfg.writeInterval
	if writeInterval <= 0 {
		writeInterval = 5 * time.Minute
	}
	checkpointInterval := cfg.checkpointInterval
	if checkpointInterval <= 0 {
		checkpointInterval = 30 * time.Minute
	}
	return &storageFlusher{
		dao:                cfg.dao,
		sqlDB:              cfg.sqlDB,
		walPath:            cfg.walPath,
		blockingManager:    cfg.blockingManager,
		savePieces:         cfg.savePieces,
		writeInterval:      writeInterval,
		checkpointInterval: checkpointInterval,
		maxBatchSize:       defaultFlusherMaxBatchSize,
		maxWALBytes:        defaultFlusherMaxWALBytes,
		persistedTotal:     cfg.persistedTotal,
		statsPersisted:     cfg.statsPersisted,
		logger:             cfg.logger,
		pendingTorrents:    make(map[protocol.ID]pendingTorrent, 256),
		pendingRescrapes:   make(map[protocol.ID]rescrapeUpdate, 256),
		triggerCh:          make(chan struct{}, 1),
		lastCheckpoint:     time.Now(),
	}
}

func (sf *storageFlusher) EnqueueTorrent(pt pendingTorrent) {
	sf.mu.Lock()
	sf.pendingTorrents[pt.torrent.InfoHash] = pt
	shouldTrigger := len(sf.pendingTorrents) >= sf.maxBatchSize
	sf.mu.Unlock()

	if shouldTrigger {
		sf.signalFlush()
	}
}

func (sf *storageFlusher) EnqueueRescrape(u rescrapeUpdate) {
	sf.mu.Lock()
	sf.pendingRescrapes[u.infoHash] = u
	shouldTrigger := len(sf.pendingRescrapes) >= sf.maxBatchSize
	sf.mu.Unlock()

	if shouldTrigger {
		sf.signalFlush()
	}
}

func (sf *storageFlusher) signalFlush() {
	select {
	case sf.triggerCh <- struct{}{}:
	default:
	}
}

func (sf *storageFlusher) PendingCounts() (torrents int, rescrapes int) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	return len(sf.pendingTorrents), len(sf.pendingRescrapes)
}

func (sf *storageFlusher) Run(ctx context.Context) {
	ticker := time.NewTicker(sf.writeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sf.Flush(ctx); err != nil && ctx.Err() == nil {
				sf.logger.Errorf("error flushing storage batch: %s", err)
			}
		case <-sf.triggerCh:
			if err := sf.Flush(ctx); err != nil && ctx.Err() == nil {
				sf.logger.Errorf("error flushing storage batch: %s", err)
			}
		}
	}
}

func (sf *storageFlusher) drainPending() ([]pendingTorrent, []rescrapeUpdate) {
	sf.mu.Lock()
	torrentsMap := sf.pendingTorrents
	rescrapesMap := sf.pendingRescrapes
	if len(torrentsMap) > 0 {
		sf.pendingTorrents = make(map[protocol.ID]pendingTorrent, 256)
	}
	if len(rescrapesMap) > 0 {
		sf.pendingRescrapes = make(map[protocol.ID]rescrapeUpdate, 256)
	}
	sf.mu.Unlock()

	torrents := make([]pendingTorrent, 0, len(torrentsMap))
	for _, pt := range torrentsMap {
		torrents = append(torrents, pt)
	}
	slices.SortFunc(torrents, func(a, b pendingTorrent) int {
		return bytes.Compare(a.torrent.InfoHash[:], b.torrent.InfoHash[:])
	})

	rescrapes := make([]rescrapeUpdate, 0, len(rescrapesMap))
	for _, u := range rescrapesMap {
		rescrapes = append(rescrapes, u)
	}
	slices.SortFunc(rescrapes, func(a, b rescrapeUpdate) int {
		return bytes.Compare(a.infoHash[:], b.infoHash[:])
	})

	return torrents, rescrapes
}

func (sf *storageFlusher) requeue(torrents []pendingTorrent, rescrapes []rescrapeUpdate) {
	if len(torrents) == 0 && len(rescrapes) == 0 {
		return
	}
	sf.mu.Lock()
	for _, pt := range torrents {
		if _, exists := sf.pendingTorrents[pt.torrent.InfoHash]; !exists {
			sf.pendingTorrents[pt.torrent.InfoHash] = pt
		}
	}
	for _, u := range rescrapes {
		if _, exists := sf.pendingRescrapes[u.infoHash]; !exists {
			sf.pendingRescrapes[u.infoHash] = u
		}
	}
	sf.mu.Unlock()
}

func (sf *storageFlusher) Flush(ctx context.Context) error {
	sf.flushMu.Lock()
	defer sf.flushMu.Unlock()

	torrents, rescrapes := sf.drainPending()
	if len(torrents) == 0 && len(rescrapes) == 0 && !sf.blockingManager.HasPending() {
		return nil
	}

	torrentsToPersist := make([]*model.Torrent, 0, len(torrents))
	var torrentFilesToPersist []*model.TorrentFile
	torrentSourcesToPersist := make([]*model.TorrentsTorrentSource, 0, len(torrents))
	var torrentPiecesToPersist []*model.TorrentPieces
	contentsByHash := make([]model.TorrentContent, 0, len(torrents))
	var archiveHashes []protocol.ID

	for _, pt := range torrents {
		t := pt.torrent
		tc := processor.NewTorrentContent(t, classify.Result{})
		tc.Torrent = model.Torrent{}
		tc.Content = model.Content{}
		contentsByHash = append(contentsByHash, tc)

		for _, f := range t.Files {
			fc := f
			torrentFilesToPersist = append(torrentFilesToPersist, &fc)
		}
		t.Files = nil

		for _, s := range t.Sources {
			sc := s
			torrentSourcesToPersist = append(torrentSourcesToPersist, &sc)
		}
		t.Sources = nil

		if sf.savePieces {
			pc := t.Pieces
			torrentPiecesToPersist = append(torrentPiecesToPersist, &pc)
			t.Pieces = model.TorrentPieces{}
		}

		torrentsToPersist = append(torrentsToPersist, &t)
		if pt.archiveAfter {
			archiveHashes = append(archiveHashes, t.InfoHash)
		}
	}

	// Keep file B-tree inserts monotonic left-to-right across the burst.
	slices.SortFunc(torrentFilesToPersist, func(a, b *model.TorrentFile) int {
		if c := bytes.Compare(a.InfoHash[:], b.InfoHash[:]); c != 0 {
			return c
		}
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})

	var onBlockCommit func()

	txErr := sf.dao.Transaction(func(tx *dao.Query) error {
		if len(torrentsToPersist) > 0 {
			if err := tx.WithContext(ctx).Torrent.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: string(sf.dao.Torrent.InfoHash.ColumnName())}},
				DoUpdates: clause.AssignmentColumns([]string{
					string(sf.dao.Torrent.Name.ColumnName()),
					string(sf.dao.Torrent.FilesStatus.ColumnName()),
					string(sf.dao.Torrent.FilesCount.ColumnName()),
					string(sf.dao.Torrent.FilesSummary.ColumnName()),
					string(sf.dao.Torrent.UpdatedAt.ColumnName()),
				}),
			}).CreateInBatches(torrentsToPersist, 100); err != nil {
				return err
			}

			if len(torrentFilesToPersist) > 0 {
				if err := tx.WithContext(ctx).TorrentFile.Clauses(clause.OnConflict{
					DoNothing: true,
				}).CreateInBatches(torrentFilesToPersist, 100); err != nil {
					return err
				}
			}

			if err := tx.TorrentsTorrentSource.WithContext(ctx).UnderlyingDB().Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: string(sf.dao.TorrentsTorrentSource.Source.ColumnName())},
					{Name: string(sf.dao.TorrentsTorrentSource.InfoHash.ColumnName())},
				},
				DoUpdates: clause.Assignments(map[string]any{
					string(sf.dao.TorrentsTorrentSource.Seeders.ColumnName()):   gorm.Expr("COALESCE(excluded.seeders, torrents_torrent_sources.seeders)"),
					string(sf.dao.TorrentsTorrentSource.Leechers.ColumnName()):  gorm.Expr("COALESCE(excluded.leechers, torrents_torrent_sources.leechers)"),
					string(sf.dao.TorrentsTorrentSource.UpdatedAt.ColumnName()): gorm.Expr("excluded.updated_at"),
				}),
			}).CreateInBatches(torrentSourcesToPersist, 100).Error; err != nil {
				return err
			}

			if sf.savePieces && len(torrentPiecesToPersist) > 0 {
				if err := tx.WithContext(ctx).TorrentPieces.Clauses(clause.OnConflict{
					DoNothing: true,
				}).CreateInBatches(torrentPiecesToPersist, 10); err != nil {
					return err
				}
			}

			if err := sf.upsertTorrentContents(ctx, tx, torrentsToPersist, contentsByHash); err != nil {
				return err
			}
		}

		connPool := tx.Torrent.WithContext(ctx).UnderlyingDB().Statement.ConnPool

		if len(rescrapes) > 0 {
			stmt, err := connPool.PrepareContext(
				ctx,
				"UPDATE torrents_torrent_sources SET seeders = ?, leechers = ?, updated_at = ? WHERE source = 'dht' AND info_hash = ?",
			)
			if err != nil {
				return err
			}
			defer stmt.Close()

			for _, r := range rescrapes {
				if _, execErr := stmt.ExecContext(ctx, r.seeders, r.leechers, r.at, r.infoHash[:]); execErr != nil {
					return execErr
				}
			}
		}

		var blockErr error
		onBlockCommit, blockErr = sf.blockingManager.PrepareFlushTx(ctx, connPool)
		return blockErr
	})
	if txErr != nil {
		sf.requeue(torrents, rescrapes)
		return txErr
	}

	if onBlockCommit != nil {
		onBlockCommit()
	}

	if len(archiveHashes) > 0 {
		if archiveErr := sf.blockingManager.Archive(ctx, archiveHashes); archiveErr != nil {
			sf.logger.Errorf("error archiving torrents: %s", archiveErr)
		}
	}

	if len(torrentsToPersist) > 0 {
		if sf.persistedTotal != nil {
			sf.persistedTotal.With(prometheus.Labels{"entity": "Torrent", "action": "insert"}).Add(float64(len(torrentsToPersist)))
			sf.persistedTotal.With(prometheus.Labels{"entity": "TorrentsTorrentSource", "action": "insert"}).Add(float64(len(torrentSourcesToPersist)))
		}
		if sf.statsPersisted != nil {
			sf.statsPersisted.Add(int64(len(torrentsToPersist)))
		}
	}
	if len(rescrapes) > 0 && sf.persistedTotal != nil {
		sf.persistedTotal.With(prometheus.Labels{"entity": "TorrentsTorrentSource", "action": "update"}).Add(float64(len(rescrapes)))
	}

	compacted, compactErr := sf.blockingManager.CompactIfNeeded(ctx, 500_000, 24*time.Hour)
	if compactErr != nil {
		sf.logger.Errorf("error compacting blocking bloom filter: %s", compactErr)
	}

	sf.maybeCheckpoint(ctx, compacted)

	return nil
}

func (sf *storageFlusher) upsertTorrentContents(
	ctx context.Context,
	tx *dao.Query,
	torrentsToPersist []*model.Torrent,
	contentsByHash []model.TorrentContent,
) error {
	existingTC := make(map[protocol.ID]struct{}, len(torrentsToPersist))
	tcDB := tx.TorrentContent.WithContext(ctx).UnderlyingDB()
	for chunk := range slices.Chunk(torrentsToPersist, 500) {
		valuers := make([]driver.Valuer, len(chunk))
		for i, t := range chunk {
			valuers[i] = t.InfoHash
		}
		var rows []protocol.ID
		if err := tcDB.Table("torrent_contents").
			Where("info_hash IN ?", valuers).
			Pluck("info_hash", &rows).Error; err != nil {
			return err
		}
		for _, h := range rows {
			existingTC[h] = struct{}{}
		}
	}

	var newContents []*model.TorrentContent
	for i := range contentsByHash {
		tc := &contentsByHash[i]
		if _, exists := existingTC[tc.InfoHash]; !exists {
			newContents = append(newContents, tc)
			continue
		}

		updates := map[string]any{
			"size":        tc.Size,
			"files_count": tc.FilesCount,
			"updated_at":  time.Now(),
		}
		if tc.Seeders.Valid {
			updates["seeders"] = tc.Seeders
		}
		if tc.Leechers.Valid {
			updates["leechers"] = tc.Leechers
		}
		if err := tcDB.Table("torrent_contents").
			Where("info_hash = ?", tc.InfoHash[:]).
			Updates(updates).Error; err != nil {
			return err
		}
	}

	if len(newContents) > 0 {
		return tx.WithContext(ctx).TorrentContent.CreateInBatches(newContents, 100)
	}
	return nil
}

func (sf *storageFlusher) maybeCheckpoint(ctx context.Context, force bool) {
	if sf.walPath == "" || sf.sqlDB == nil {
		return
	}

	needCheckpoint := force ||
		(sf.checkpointInterval > 0 && time.Since(sf.lastCheckpoint) >= sf.checkpointInterval) ||
		(sf.maxWALBytes > 0 && walFileSize(sf.walPath) >= sf.maxWALBytes)

	if !needCheckpoint {
		return
	}

	if _, err := sf.sqlDB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		sf.logger.Warnw("wal checkpoint failed", "error", err)
		return
	}
	sf.lastCheckpoint = time.Now()
}

func walFileSize(walPath string) int64 {
	info, err := os.Stat(walPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

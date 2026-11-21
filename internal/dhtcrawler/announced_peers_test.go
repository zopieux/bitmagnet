package dhtcrawler

import (
	"context"
	"database/sql"
	"net/netip"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/concurrency"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
	ktable_mocks "github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable/mocks"
	_ "github.com/glebarez/go-sqlite"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	boom "github.com/tylertreat/BoomFilters"
	"go.uber.org/zap"
)

func newTestBlockingManager(t *testing.T) blocking.Manager {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`CREATE TABLE torrents (info_hash BLOB PRIMARY KEY, name TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE bloom_filters (key TEXT PRIMARY KEY, data BLOB, created_at DATETIME, updated_at DATETIME)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE blocked_hash_chunks (id INTEGER PRIMARY KEY, hashes BLOB NOT NULL, created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	return blocking.NewManager(db, "", time.Minute)
}

func TestAnnouncedPeersTriageRouting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bm := newTestBlockingManager(t)
	kh := newKnownHashes()
	tableMock := ktable_mocks.NewTable(t)

	announcedCh := make(chan dht.AnnouncedPeer, 16)
	metaReqCh := concurrency.NewBufferedConcurrentChannel[infoHashWithPeers](16, 4)
	getPeersCh := concurrency.NewBufferedConcurrentChannel[nodeHasPeersForHash](16, 4)
	rescrapeCh := concurrency.NewBufferedConcurrentChannel[nodeHasPeersForHash](16, 4)

	c := &crawler{
		kTable:            tableMock,
		infoHashTriage:    concurrency.NewBatchingChannel[nodeHasPeersForHash](32, 1, 5*time.Millisecond),
		requestMetaInfo:   metaReqCh,
		getPeers:          getPeersCh,
		rescrape:          rescrapeCh,
		rescrapeThreshold: 30 * 24 * time.Hour,
		ignoreHashes: &ignoreHashes{
			bloom: boom.NewStableBloomFilter(10_000, 2, 0.001),
		},
		knownHashes:     kh,
		blockingManager: bm,
		announcedPeers:  announcedCh,
		logger:          zap.NewNop().Sugar(),
	}

	gotMeta := make(chan infoHashWithPeers, 16)
	gotPeers := make(chan nodeHasPeersForHash, 16)
	gotRescrape := make(chan nodeHasPeersForHash, 16)

	go func() {
		_ = metaReqCh.Run(ctx, func(item infoHashWithPeers) { gotMeta <- item })
	}()
	go func() {
		_ = getPeersCh.Run(ctx, func(item nodeHasPeersForHash) { gotPeers <- item })
	}()
	go func() {
		_ = rescrapeCh.Run(ctx, func(item nodeHasPeersForHash) { gotRescrape <- item })
	}()
	go c.runAnnouncedPeers(ctx)
	go c.runInfoHashTriage(ctx)

	var (
		unknownHash   = protocol.RandomNodeID()
		blockedHash   = protocol.RandomNodeID()
		freshHash     = protocol.RandomNodeID()
		staleHash     = protocol.RandomNodeID()
		noPeerHash    = protocol.RandomNodeID()
		announcerNode = netip.MustParseAddrPort("198.51.100.10:6881")
		announcerPeer = netip.MustParseAddrPort("198.51.100.10:51413")
		closestAddr   = netip.MustParseAddrPort("203.0.113.5:6881")
	)

	bm.BlockUnpersisted([]protocol.ID{blockedHash})
	kh.MarkKnown(freshHash, encodeDay(time.Now()))
	kh.MarkKnown(staleHash, encodeDay(time.Now().Add(-31*24*time.Hour)))

	closestNode := ktable.NewNode(protocol.RandomNodeID(), closestAddr)
	tableMock.On("GetClosestNodes", mock.AnythingOfType("protocol.ID")).Return([]ktable.Node{closestNode})

	// Send unknownHash twice to verify ignoreHashes deduplicates it before triage.
	announcedCh <- dht.AnnouncedPeer{InfoHash: unknownHash, Node: announcerNode, Peer: announcerPeer}
	announcedCh <- dht.AnnouncedPeer{InfoHash: unknownHash, Node: announcerNode, Peer: announcerPeer}
	announcedCh <- dht.AnnouncedPeer{InfoHash: blockedHash, Node: announcerNode, Peer: announcerPeer}
	announcedCh <- dht.AnnouncedPeer{InfoHash: freshHash, Node: announcerNode, Peer: announcerPeer}
	announcedCh <- dht.AnnouncedPeer{InfoHash: staleHash, Node: announcerNode, Peer: announcerPeer}
	announcedCh <- dht.AnnouncedPeer{InfoHash: noPeerHash, Node: announcerNode}

	select {
	case req := <-gotMeta:
		assert.Equal(t, unknownHash, req.infoHash)
		assert.Equal(t, closestAddr, req.node)
		assert.Equal(t, []netip.AddrPort{announcerPeer}, req.peers)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for announced unknown torrent on requestMetaInfo")
	}

	select {
	case req := <-gotPeers:
		assert.Equal(t, noPeerHash, req.infoHash)
		assert.Equal(t, closestAddr, req.node)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for no-peer announced torrent fallback on getPeers")
	}

	select {
	case req := <-gotRescrape:
		assert.Equal(t, staleHash, req.infoHash)
		assert.Equal(t, closestAddr, req.node)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stale announced torrent on rescrape")
	}

	// Allow any stray sends to surface before asserting emptiness.
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, gotMeta)
	assert.Empty(t, gotPeers)
	assert.Empty(t, gotRescrape)
	assert.Equal(t, int64(6), c.statsAnnounced.Load())
	assert.Equal(t, int64(2), c.statsNew.Load())
}

func TestRunScrapeNewWithoutScrapeNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q, sqlDB, _ := setupFileBackedFlusherTestDB(t)
	bm := blocking.NewManager(sqlDB, "", time.Minute)
	persistedTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_scrape_new_persisted_total",
		Help: "test",
	}, []string{"entity", "action"})

	sf := newStorageFlusher(storageFlusherConfig{
		dao:                q,
		sqlDB:              sqlDB,
		blockingManager:    bm,
		writeInterval:      time.Minute,
		checkpointInterval: 30 * time.Minute,
		persistedTotal:     persistedTotal,
		logger:             zap.NewNop().Sugar(),
	})

	c := &crawler{
		scrapeNew:      concurrency.NewBufferedConcurrentChannel[pendingTorrent](4, 2),
		storageFlusher: sf,
		logger:         zap.NewNop().Sugar(),
	}

	go c.runScrapeNew(ctx)

	h := protocol.RandomNodeID()
	c.scrapeNew.In() <- pendingTorrent{
		nodeHasPeersForHash: nodeHasPeersForHash{
			infoHash: h,
			peer:     netip.MustParseAddrPort("198.51.100.10:51413"),
		},
		torrent: model.Torrent{
			InfoHash: h,
			Name:     "announced-no-knode",
			Sources: []model.TorrentsTorrentSource{
				{Source: "dht", InfoHash: h},
			},
		},
	}

	require.Eventually(t, func() bool {
		n, _ := sf.PendingCounts()
		return n == 1
	}, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, sf.Flush(ctx))

	var count int
	require.NoError(t, sqlDB.QueryRow(`SELECT COUNT(*) FROM torrents WHERE info_hash = ?`, h[:]).Scan(&count))
	assert.Equal(t, 1, count)
}

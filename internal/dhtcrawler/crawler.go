package dhtcrawler

import (
	"context"
	"database/sql"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/bloom"
	"github.com/bitmagnet-io/bitmagnet/internal/classify"
	"github.com/bitmagnet-io/bitmagnet/internal/concurrency"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/client"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/banning"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/metainforequester"
	"github.com/prometheus/client_golang/prometheus"
	boom "github.com/tylertreat/BoomFilters"
	"go.uber.org/zap"
)

type crawler struct {
	kTable                       ktable.Table
	client                       client.Client
	metainfoRequester            metainforequester.Requester
	banningChecker               banning.Checker
	classifyConfig               classify.Config
	sqlDB                        *sql.DB
	bootstrapNodes               []string
	reseedBootstrapNodesInterval time.Duration
	getOldestNodesInterval       time.Duration
	oldPeerThreshold             time.Duration
	discoveredNodes              concurrency.BatchingChannel[ktable.Node]
	announcedPeers               <-chan dht.AnnouncedPeer
	nodesForPing                 concurrency.BufferedConcurrentChannel[ktable.Node]
	nodesForFindNode             concurrency.BufferedConcurrentChannel[ktable.Node]
	nodesForSampleInfoHashes     concurrency.BufferedConcurrentChannel[ktable.Node]
	infoHashTriage               concurrency.BatchingChannel[nodeHasPeersForHash]
	getPeers                     concurrency.BufferedConcurrentChannel[nodeHasPeersForHash]
	requestMetaInfo              concurrency.BufferedConcurrentChannel[infoHashWithPeers]
	scrapeNew                    concurrency.BufferedConcurrentChannel[pendingTorrent]
	rescrape                     concurrency.BufferedConcurrentChannel[nodeHasPeersForHash]
	storageFlusher               *storageFlusher
	rescrapeThreshold            time.Duration
	saveFilesThreshold           uint
	savePieces                   bool
	dao                          *dao.Query
	// ignoreHashes is a thread-safe bloom filter that the crawler keeps in memory,
	// containing every hash it has already encountered in this process lifetime.
	ignoreHashes    *ignoreHashes
	knownHashes     *knownHashes
	blockingManager blocking.Manager
	// soughtNodeID is a random node ID used as the target for find_node and sample_infohashes requests.
	// It is rotated every 10 seconds.
	soughtNodeID   *concurrency.AtomicValue[protocol.ID]
	probeCursor    protocol.ID
	stopped        chan struct{}
	done           chan struct{}
	persistedTotal *prometheus.CounterVec
	logger         *zap.SugaredLogger
	// stats counters for the periodic crawler stats log line
	statsSampled   atomic.Int64
	statsAnnounced atomic.Int64
	statsNew       atomic.Int64
	statsPersisted atomic.Int64
	statsBlocked   atomic.Int64
	statsMetaOk    atomic.Int64
	statsMetaErr   atomic.Int64
}

func (c *crawler) start() {
	defer close(c.done)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel immediately on stop so startup loads don't delay shutdown.
	go func() {
		select {
		case <-c.stopped:
			cancel()
		case <-ctx.Done():
		}
	}()

	c.backfillMultiFileSummaries(ctx)
	if ctx.Err() != nil {
		return
	}

	loadStart := time.Now()
	if err := c.knownHashes.Load(ctx, c.sqlDB, c.saveFilesThreshold); err != nil {
		if ctx.Err() == nil {
			c.logger.Errorf("failed to load known hashes: %s", err)
		}
	} else {
		c.logger.Infow("loaded known hashes into memory",
			"count", c.knownHashes.Len(),
			"duration", time.Since(loadStart).Round(time.Millisecond),
		)
	}
	if ctx.Err() != nil {
		return
	}

	// start the various pipeline workers
	go c.rotateSoughtNodeID(ctx)
	go c.runDiscoveredNodes(ctx)
	go c.runAnnouncedPeers(ctx)
	go c.runPing(ctx)
	go c.runFindNode(ctx)
	go c.getNodesForFindNode(ctx)
	go c.runSampleInfoHashes(ctx)
	go c.getNodesForSampleInfoHashes(ctx)
	go c.runInfoHashTriage(ctx)
	go c.runGetPeers(ctx)
	go c.runRequestMetaInfo(ctx)
	go c.runScrapeNew(ctx)
	go c.runRescrape(ctx)
	go c.reseedBootstrapNodes(ctx)
	go c.storageFlusher.Run(ctx)
	go c.getOldNodes(ctx)
	go c.runStatsLogger(ctx)
	go c.runMissingSummaryProbe(ctx)
	<-c.stopped
	cancel()

	flushCtx, flushCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer flushCancel()
	if err := c.storageFlusher.Flush(flushCtx); err != nil {
		c.logger.Errorf("error flushing buffered writes on shutdown: %s", err)
	}
}

// runStatsLogger logs crawler throughput counters every 30s so a silent or
// stalled pipeline is visible in the logs (deltas since the previous line).
func (c *crawler) runStatsLogger(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var prevSampled, prevAnnounced, prevNew, prevPersisted, prevBlocked, prevMetaOk, prevMetaErr int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sampled := c.statsSampled.Load()
			announced := c.statsAnnounced.Load()
			newHashes := c.statsNew.Load()
			persisted := c.statsPersisted.Load()
			blocked := c.statsBlocked.Load()
			metaOk := c.statsMetaOk.Load()
			metaErr := c.statsMetaErr.Load()
			c.logger.Infof(
				"crawler stats (last 30s): sampled=%d announced=%d new=%d persisted=%d blocked=%d metadata_ok=%d metadata_err=%d",
				sampled-prevSampled, announced-prevAnnounced, newHashes-prevNew, persisted-prevPersisted, blocked-prevBlocked,
				metaOk-prevMetaOk, metaErr-prevMetaErr,
			)
			prevSampled, prevAnnounced, prevNew, prevPersisted, prevBlocked, prevMetaOk, prevMetaErr = sampled, announced, newHashes, persisted, blocked, metaOk, metaErr
		}
	}
}

type nodeHasPeersForHash struct {
	infoHash  protocol.ID
	node      netip.AddrPort
	peer      netip.AddrPort
	fromProbe bool
}

type infoHashWithPeers struct {
	nodeHasPeersForHash
	peers []netip.AddrPort
}

type infoHashWithScrape struct {
	nodeHasPeersForHash
	bfsd bloom.Filter
	bfpe bloom.Filter
}

func (s infoHashWithScrape) seedersLeechers() (model.NullUint, model.NullUint) {
	return model.NewNullUint(uint(s.bfsd.ApproximatedSize())),
		model.NewNullUint(uint(s.bfpe.ApproximatedSize()))
}

type ignoreHashes struct {
	mutex sync.Mutex
	bloom *boom.StableBloomFilter
}

func (i *ignoreHashes) testAndAdd(id protocol.ID) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	return i.bloom.TestAndAdd(id[:])
}

func (c *crawler) rotateSoughtNodeID(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			c.soughtNodeID.Set(protocol.RandomNodeID())
		}
	}
}

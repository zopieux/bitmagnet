package dhtcrawler

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/bloom"
	"github.com/bitmagnet-io/bitmagnet/internal/concurrency"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/client"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/banning"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/metainforequester"
	queueprometheus "github.com/bitmagnet-io/bitmagnet/internal/queue/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	boom "github.com/tylertreat/BoomFilters"
	"go.uber.org/zap"
)

type crawler struct {
	kTable                       ktable.Table
	client                       client.Client
	metainfoRequester            metainforequester.Requester
	banningChecker               banning.Checker
	bootstrapNodes               []string
	reseedBootstrapNodesInterval time.Duration
	getOldestNodesInterval       time.Duration
	oldPeerThreshold             time.Duration
	discoveredNodes              concurrency.BatchingChannel[ktable.Node]
	nodesForPing                 concurrency.BufferedConcurrentChannel[ktable.Node]
	nodesForFindNode             concurrency.BufferedConcurrentChannel[ktable.Node]
	nodesForSampleInfoHashes     concurrency.BufferedConcurrentChannel[ktable.Node]
	infoHashTriage               concurrency.BatchingChannel[nodeHasPeersForHash]
	getPeers                     concurrency.BufferedConcurrentChannel[nodeHasPeersForHash]
	scrape                       concurrency.BufferedConcurrentChannel[nodeHasPeersForHash]
	requestMetaInfo              concurrency.BufferedConcurrentChannel[infoHashWithPeers]
	persistTorrents              concurrency.BatchingChannel[infoHashWithMetaInfo]
	persistSources               concurrency.BatchingChannel[infoHashWithScrape]
	rescrapeThreshold            time.Duration
	saveFilesThreshold           uint
	savePieces                   bool
	dao                          *dao.Query
	// ignoreHashes is a thread-safe bloom filter that the crawler keeps in memory,
	// containing every hash it has already encountered.
	// This avoids multiple attempts to crawl the same hash, and takes a lot of load off the database query
	// that checks if a hash has already been indexed.
	ignoreHashes    *ignoreHashes
	blockingManager blocking.Manager
	// soughtNodeID is a random node ID used as the target for find_node and sample_infohashes requests.
	// It is rotated every 10 seconds.
	soughtNodeID   *concurrency.AtomicValue[protocol.ID]
	probeCursor    protocol.ID
	stopped        chan struct{}
	persistedTotal *prometheus.CounterVec
	queueMetrics   queueprometheus.QueueMetrics
	logger         *zap.SugaredLogger
	// stats counters for the periodic crawler stats log line
	statsSampled   atomic.Int64
	statsNew       atomic.Int64
	statsPersisted atomic.Int64
	statsMetaOk    atomic.Int64
	statsMetaErr   atomic.Int64
}

func (c *crawler) start() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.backfillMultiFileSummaries(ctx)

	// start the various pipeline workers
	go c.rotateSoughtNodeID(ctx)
	go c.runDiscoveredNodes(ctx)
	go c.runPing(ctx)
	go c.runFindNode(ctx)
	go c.getNodesForFindNode(ctx)
	go c.runSampleInfoHashes(ctx)
	go c.getNodesForSampleInfoHashes(ctx)
	go c.runInfoHashTriage(ctx)
	go c.runGetPeers(ctx)
	go c.runRequestMetaInfo(ctx)
	go c.runScrape(ctx)
	go c.reseedBootstrapNodes(ctx)
	go c.runPersistTorrents(ctx)
	go c.runPersistSources(ctx)
	go c.getOldNodes(ctx)
	go c.runStatsLogger(ctx)
	go c.runMissingSummaryProbe(ctx)
	<-c.stopped
}

// runStatsLogger logs crawler throughput counters every 30s so a silent or
// stalled pipeline is visible in the logs (deltas since the previous line).
func (c *crawler) runStatsLogger(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var prevSampled, prevNew, prevPersisted, prevMetaOk, prevMetaErr int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sampled := c.statsSampled.Load()
			newHashes := c.statsNew.Load()
			persisted := c.statsPersisted.Load()
			metaOk := c.statsMetaOk.Load()
			metaErr := c.statsMetaErr.Load()
			c.logger.Infof(
				"crawler stats (last 30s): sampled=%d new=%d persisted=%d metadata_ok=%d metadata_err=%d",
				sampled-prevSampled, newHashes-prevNew, persisted-prevPersisted,
				metaOk-prevMetaOk, metaErr-prevMetaErr,
			)
			prevSampled, prevNew, prevPersisted, prevMetaOk, prevMetaErr = sampled, newHashes, persisted, metaOk, metaErr
		}
	}
}

type nodeHasPeersForHash struct {
	infoHash protocol.ID
	node     netip.AddrPort
}

type infoHashWithMetaInfo struct {
	nodeHasPeersForHash
	metaInfo metainfo.Info
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

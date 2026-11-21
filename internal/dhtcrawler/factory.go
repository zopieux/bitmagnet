package dhtcrawler

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/classify"
	"github.com/bitmagnet-io/bitmagnet/internal/concurrency"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/client"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht/ktable"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/banning"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo/metainforequester"
	"github.com/bitmagnet-io/bitmagnet/internal/worker"
	"github.com/prometheus/client_golang/prometheus"
	boom "github.com/tylertreat/BoomFilters"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Params struct {
	fx.In
	Config            Config
	ClassifyConfig    classify.Config
	SqliteConfig      sqlite.Config
	DB                lazy.Lazy[*sql.DB]
	KTable            ktable.Table
	Client            lazy.Lazy[client.Client]
	MetainfoRequester metainforequester.Requester
	BanningChecker    banning.Checker `name:"metainfo_banning_checker"`
	Dao               lazy.Lazy[*dao.Query]
	BlockingManager   lazy.Lazy[blocking.Manager]
	DiscoveredNodes   concurrency.BatchingChannel[ktable.Node] `name:"dht_discovered_nodes"`
	AnnouncedPeers    chan dht.AnnouncedPeer                   `name:"dht_announced_peers" optional:"true"`
	Logger            *zap.SugaredLogger
}

type Result struct {
	fx.Out
	Worker worker.Worker `group:"workers"`

	DhtCrawlerActive *concurrency.AtomicValue[bool] `name:"dht_crawler_active"`

	PersistedTotal prometheus.Collector `group:"prometheus_collectors"`

	StageQueueDepth prometheus.Collector `group:"prometheus_collectors"`
}

func New(params Params) Result {
	active := &concurrency.AtomicValue[bool]{}

	var c crawler

	persistedTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "bitmagnet",
		Subsystem: "dht_crawler",
		Name:      "persisted_total",
		Help:      "A counter of persisted database entities.",
	}, []string{"entity", "action"})

	stageDepth := newStageDepthCollector()

	return Result{
		Worker: worker.NewWorker(
			"dht_crawler",
			fx.Hook{
				OnStart: func(context.Context) error {
					active.Set(true)
					scalingFactor := int(params.Config.ScalingFactor)
					cl, err := params.Client.Get()
					if err != nil {
						return err
					}
					sqlDB, err := params.DB.Get()
					if err != nil {
						return err
					}
					query, err := params.Dao.Get()
					if err != nil {
						return err
					}
					blockingManager, err := params.BlockingManager.Get()
					if err != nil {
						return err
					}

					triageInterval := params.Config.InfoHashTriageInterval
					if triageInterval <= 0 || triageInterval > time.Second {
						triageInterval = time.Second
					}

					dbPath := sqlite.DBPath(params.SqliteConfig)
					basePath, _, _ := strings.Cut(dbPath, "?")
					walPath := ""
					if basePath != "" && basePath != ":memory:" {
						walPath = basePath + "-wal"
					}

					logger := params.Logger.Named("dht_crawler")

					c = crawler{
						kTable:                       params.KTable,
						client:                       cl,
						metainfoRequester:            params.MetainfoRequester,
						banningChecker:               params.BanningChecker,
						classifyConfig:               params.ClassifyConfig,
						sqlDB:                        sqlDB,
						bootstrapNodes:               params.Config.BootstrapNodes,
						reseedBootstrapNodesInterval: params.Config.ReseedBootstrapNodesInterval,
						getOldestNodesInterval:       time.Second * 10,
						oldPeerThreshold:             time.Minute * 15,
						discoveredNodes:              params.DiscoveredNodes,
						announcedPeers:               params.AnnouncedPeers,
						nodesForPing: concurrency.NewBufferedConcurrentChannel[ktable.Node](
							scalingFactor, scalingFactor),
						nodesForFindNode: concurrency.NewBufferedConcurrentChannel[ktable.Node](
							10*scalingFactor, 10*scalingFactor),
						nodesForSampleInfoHashes: concurrency.NewBufferedConcurrentChannel[ktable.Node](
							10*scalingFactor,
							10*scalingFactor,
						),
						infoHashTriage: concurrency.NewBatchingChannel[nodeHasPeersForHash](
							10*scalingFactor, 1000, triageInterval),
						getPeers: concurrency.NewBufferedConcurrentChannel[nodeHasPeersForHash](
							10*scalingFactor, 20*scalingFactor),
						requestMetaInfo: concurrency.NewBufferedConcurrentChannel[infoHashWithPeers](
							10*scalingFactor,
							40*scalingFactor,
						),
						scrapeNew: concurrency.NewBufferedConcurrentChannel[pendingTorrent](
							10*scalingFactor, 20*scalingFactor),
						rescrape: concurrency.NewBufferedConcurrentChannel[nodeHasPeersForHash](
							10*scalingFactor, 20*scalingFactor),
						saveFilesThreshold: params.Config.SaveFilesThreshold,
						savePieces:         params.Config.SavePieces,
						rescrapeThreshold:  params.Config.RescrapeThreshold,
						dao:                query,
						ignoreHashes: &ignoreHashes{
							bloom: boom.NewStableBloomFilter(10_000_000, 2, 0.001),
						},
						knownHashes:     newKnownHashes(),
						blockingManager: blockingManager,
						soughtNodeID:    &concurrency.AtomicValue[protocol.ID]{},
						stopped:         make(chan struct{}),
						done:            make(chan struct{}),
						persistedTotal:  persistedTotal,
						logger:          logger,
					}
					c.storageFlusher = newStorageFlusher(storageFlusherConfig{
						dao:                query,
						sqlDB:              sqlDB,
						walPath:            walPath,
						blockingManager:    blockingManager,
						savePieces:         params.Config.SavePieces,
						writeInterval:      params.SqliteConfig.WriteInterval,
						checkpointInterval: params.SqliteConfig.CheckpointInterval,
						persistedTotal:     persistedTotal,
						statsPersisted:     &c.statsPersisted,
						logger:             logger,
					})
					c.soughtNodeID.Set(protocol.RandomNodeID())

					stageDepth.setProbes(map[string]func() int{
						"discovered_nodes":  func() int { return len(c.discoveredNodes.In()) },
						"announced_peers":   func() int { return len(c.announcedPeers) },
						"ping":              func() int { return len(c.nodesForPing.In()) },
						"find_node":         func() int { return len(c.nodesForFindNode.In()) },
						"sample_infohashes": func() int { return len(c.nodesForSampleInfoHashes.In()) },
						"triage":            func() int { return len(c.infoHashTriage.In()) },
						"get_peers":         func() int { return len(c.getPeers.In()) },
						"scrape_new":        func() int { return len(c.scrapeNew.In()) },
						"rescrape":          func() int { return len(c.rescrape.In()) },
						"request_meta_info": func() int { return len(c.requestMetaInfo.In()) },
						"flush_torrents": func() int {
							n, _ := c.storageFlusher.PendingCounts()
							return n
						},
						"flush_rescrapes": func() int {
							_, n := c.storageFlusher.PendingCounts()
							return n
						},
					})

					// todo: Fix!
					//nolint:contextcheck
					go c.start()
					return nil
				},
				OnStop: func(ctx context.Context) error {
					active.Set(false)
					if c.stopped != nil {
						close(c.stopped)
					}
					if c.done != nil {
						waitCtx := ctx
						if _, hasDeadline := ctx.Deadline(); !hasDeadline {
							var cancel context.CancelFunc
							waitCtx, cancel = context.WithTimeout(ctx, 35*time.Second)
							defer cancel()
						}
						select {
						case <-c.done:
						case <-waitCtx.Done():
							return waitCtx.Err()
						}
					}
					return nil
				},
			},
		),
		PersistedTotal:   persistedTotal,
		StageQueueDepth:  stageDepth,
		DhtCrawlerActive: active,
	}
}

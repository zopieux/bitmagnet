package blocking

import (
	"context"
	"database/sql"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"
)

type Params struct {
	fx.In
	DB           lazy.Lazy[*sql.DB]
	SqliteConfig sqlite.Config
	Lifecycle    fx.Lifecycle `optional:"true"`
}

type Result struct {
	fx.Out
	Manager      lazy.Lazy[Manager]
	BlockedTotal prometheus.Collector `group:"prometheus_collectors"`
	SkippedTotal prometheus.Collector `group:"prometheus_collectors"`
}

func New(params Params) Result {
	blockedTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "bitmagnet",
		Subsystem: "blocking",
		Name:      "blocked_total",
		Help:      "Total number of torrents newly marked as blocked.",
	}, []string{"action"})
	blockedTotal.WithLabelValues("blocked")
	blockedTotal.WithLabelValues("archived")

	skippedTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "bitmagnet",
		Subsystem: "blocking",
		Name:      "skipped_total",
		Help:      "Total number of already-blocked infohashes skipped by the filter.",
	})

	lazyManager := lazy.New(func() (Manager, error) {
		db, err := params.DB.Get()
		if err != nil {
			return nil, err
		}

		dbPath := sqlite.DBPath(params.SqliteConfig)

		return NewManager(
			db,
			sqlite.ArchiveDBPath(dbPath),
			params.SqliteConfig.WriteInterval,
			WithMetrics(blockedTotal, skippedTotal),
		), nil
	})

	if params.Lifecycle != nil {
		params.Lifecycle.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				return lazyManager.IfInitialized(func(m Manager) error {
					flushCtx := ctx
					if _, hasDeadline := ctx.Deadline(); !hasDeadline {
						var cancel context.CancelFunc
						flushCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
						defer cancel()
					}
					return m.Compact(flushCtx)
				})
			},
		})
	}

	return Result{
		Manager:      lazyManager,
		BlockedTotal: blockedTotal,
		SkippedTotal: skippedTotal,
	}
}

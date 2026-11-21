package server

import (
	"context"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/queue/handler"
	"github.com/bitmagnet-io/bitmagnet/internal/queue/prometheus"
	"github.com/bitmagnet-io/bitmagnet/internal/worker"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Params struct {
	fx.In
	Query lazy.Lazy[*dao.Query]
	// PgxPool  lazy.Lazy[*pgxpool.Pool]
	Handlers []lazy.Lazy[handler.Handler] `group:"queue_handlers"`
	Logger       *zap.SugaredLogger
	SqliteConfig sqlite.Config
	Metrics      prometheus.QueueMetrics `optional:"true"`
}

type Result struct {
	fx.Out
	Worker worker.Worker `group:"workers"`
}

func New(p Params) Result {
	stopped := make(chan struct{})
	var srv *server

	return Result{
		Worker: worker.NewWorker(
			"queue_server",
			fx.Hook{
				OnStart: func(context.Context) error {
					// pool, err := p.PgxPool.Get()
					// if err != nil {
					// 	return err
					// }
					query, err := p.Query.Get()
					if err != nil {
						return err
					}
					handlers := make([]handler.Handler, 0, len(p.Handlers))
					for _, lh := range p.Handlers {
						h, err := lh.Get()
						if err != nil {
							return err
						}
						handlers = append(handlers, h)
					}
					checkInterval := p.SqliteConfig.WriteInterval
					if checkInterval < 5*time.Minute {
						checkInterval = 5 * time.Minute
					}
					srv = &server{
						stopped: stopped,
						query:   query,
						// pool:       pool,
						handlers:      handlers,
						gcInterval:    6 * time.Hour,
						checkInterval: checkInterval,
						logger:        p.Logger.Named("queue"),
						metrics:       p.Metrics,
					}
					// todo: Fix!
					//nolint:contextcheck
					return srv.Start(context.Background())
				},
				OnStop: func(ctx context.Context) error {
					close(stopped)
					if srv != nil {
						return srv.Wait(ctx)
					}
					return nil
				},
			},
		),
	}
}

package blocking

import (
	"context"
	"database/sql"
	"sync"

	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"go.uber.org/fx"
)

type Params struct {
	fx.In
	DB           lazy.Lazy[*sql.DB]
	SqliteConfig sqlite.Config
	FlushWait    *sync.WaitGroup `name:"db_flush_wait"`
}

type Result struct {
	fx.Out
	Manager lazy.Lazy[Manager]
	AppHook fx.Hook `group:"app_hooks"`
}

func New(params Params) Result {
	params.FlushWait.Add(1)

	lazyManager := lazy.New[Manager](func() (Manager, error) {
		db, err := params.DB.Get()
		if err != nil {
			return nil, err
		}

		return &manager{
			db:            db,
			buffer:        make(map[protocol.ID]struct{}, 1000),
			maxBufferSize: 1000,
			maxFlushWait:  params.SqliteConfig.WriteInterval,
		}, nil
	})

	return Result{
		Manager: lazyManager,
		AppHook: fx.Hook{
			OnStop: func(ctx context.Context) error {
				defer params.FlushWait.Done()
				return lazyManager.IfInitialized(func(m Manager) error {
					return m.Flush(ctx)
				})
			},
		},
	}
}

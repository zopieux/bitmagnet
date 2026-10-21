package processor

import (
	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/database/dao"
	"github.com/bitmagnet-io/bitmagnet/internal/database/search"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/tmdb"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Params struct {
	fx.In
	TmdbClient      lazy.Lazy[tmdb.Client]
	TmdbConfig      tmdb.Config
	Search          lazy.Lazy[search.Search]
	Dao             lazy.Lazy[*dao.Query]
	BlockingManager lazy.Lazy[blocking.Manager]
	Logger          *zap.SugaredLogger
}

type Result struct {
	fx.Out
	Processor lazy.Lazy[Processor]
}

func New(p Params) Result {
	return Result{
		Processor: lazy.New(func() (Processor, error) {
			s, err := p.Search.Get()
			if err != nil {
				return nil, err
			}
			d, err := p.Dao.Get()
			if err != nil {
				return nil, err
			}
			bm, err := p.BlockingManager.Get()
			if err != nil {
				return nil, err
			}
			tc, err := p.TmdbClient.Get()
			if err != nil {
				return nil, err
			}

			// TMDB lookup is skipped if no API key is configured.
			tmdbEnabled := p.TmdbConfig.Enabled && p.TmdbConfig.APIKey != ""

			return processor{
				dao:             d,
				search:          s,
				blockingManager: bm,
				tmdbClient:      tc,
				tmdbEnabled:     tmdbEnabled,
			}, nil
		}),
	}
}

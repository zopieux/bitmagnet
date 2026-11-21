package app

import (
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/app/appfx"
	"github.com/bitmagnet-io/bitmagnet/internal/app/cli/hooks"
	"github.com/bitmagnet-io/bitmagnet/internal/logging/loggingfx"
	"github.com/urfave/cli/v2"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

func New() *fx.App {
	return fx.New(
		fx.StopTimeout(60*time.Second),
		appfx.New(),
		loggingfx.WithLogger(),
		fx.Invoke(func(
			logger *zap.SugaredLogger,
			_ hooks.AttachedHooks,
			_ *cli.App,
		) {
			logger.Debug("app invoked")
		}),
	)
}

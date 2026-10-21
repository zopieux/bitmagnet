package devfx

import (
	"github.com/bitmagnet-io/bitmagnet/internal/app/cli"
	"github.com/bitmagnet-io/bitmagnet/internal/app/cli/args"
	"github.com/bitmagnet-io/bitmagnet/internal/config/configfx"
	"github.com/bitmagnet-io/bitmagnet/internal/database"
	"github.com/bitmagnet-io/bitmagnet/internal/database/migrations"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/dev/app/cmd/gormcmd"
	"github.com/bitmagnet-io/bitmagnet/internal/dev/app/cmd/migratecmd"
	"github.com/bitmagnet-io/bitmagnet/internal/logging/loggingfx"
	"github.com/bitmagnet-io/bitmagnet/internal/validation/validationfx"
	"go.uber.org/fx"
)

func New() fx.Option {
	return fx.Module(
		"dev",
		configfx.NewConfigModule[sqlite.Config]("db", sqlite.NewDefaultConfig()),
		configfx.New(),
		loggingfx.New(),
		validationfx.New(),
		fx.Provide(args.New),
		fx.Provide(cli.New),
		fx.Provide(database.New),
		fx.Provide(migrations.New),
		fx.Provide(sqlite.New),
		fx.Provide(gormcmd.New),
		fx.Provide(migratecmd.New),
	)
}

package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"

	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	_ "github.com/glebarez/go-sqlite"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Params struct {
	fx.In
	Config Config
	Logger *zap.SugaredLogger
}

type Result struct {
	fx.Out
	SQLDB     lazy.Lazy[*sql.DB]
	FlushWait *sync.WaitGroup `name:"db_flush_wait"`
	AppHook   fx.Hook         `group:"app_hooks"`
}

func New(p Params) (Result, error) {
	flushWait := &sync.WaitGroup{}

	dbPath := p.Config.DSN
	if dbPath == "" {
		dbPath = p.Config.Name
		if dbPath == "" || dbPath == "bitmagnet" {
			dbPath = "bitmagnet.db"
		}
	}

	lazyDB := lazy.New(func() (*sql.DB, error) {
		if dbPath != ":memory:" {
			dir := filepath.Dir(dbPath)
			if dir != "" && dir != "." {
				_ = os.MkdirAll(dir, 0755)
			}
		}
		db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(OFF)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)&_pragma=auto_vacuum(INCREMENTAL)")
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(1)
		return db, nil
	})

	return Result{
		SQLDB:     lazyDB,
		FlushWait: flushWait,
		AppHook: fx.Hook{
			OnStart: func(context.Context) error {
				if dbPath == ":memory:" {
					return nil
				}
				db, err := lazyDB.Get()
				if err != nil {
					return err
				}
				return ensureIncrementalAutoVacuum(db, p.Logger)
			},
			OnStop: func(context.Context) error {
				// Wait for the blocking manager to flush before closing.
				// If the blocking module is not part of the app (e.g. dev
				// commands), nothing ever calls Add and this returns at once.
				flushWait.Wait()
				if db, err := lazyDB.Get(); err == nil {
					_ = db.Close()
				}
				return nil
			},
		},
	}, nil
}

// ensureIncrementalAutoVacuum converts an existing database to incremental
// auto-vacuum. The auto_vacuum mode is stored in the database header and
// changing it only takes effect after a VACUUM, so this runs a one-time
// VACUUM at startup for databases created before the DSN pragma existed.
// New databases are born incremental via the DSN pragma and skip this.
// The periodic incremental vacuum itself runs in the queue server's
// maintenance tick, batched with the other database housekeeping.
func ensureIncrementalAutoVacuum(db *sql.DB, logger *zap.SugaredLogger) error {
	var mode int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return err
	}
	if mode == 2 { // INCREMENTAL
		return nil
	}
	logger.Infow("converting database to incremental auto-vacuum (one-time VACUUM)")
	if _, err := db.Exec("PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
		return err
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return err
	}
	return nil
}

package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	_ "github.com/glebarez/go-sqlite"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var defaultPragmas = []string{
	"foreign_keys(1)",
	"journal_mode(WAL)",
	"busy_timeout(5000)",
	"synchronous(NORMAL)",
	"cache_size(-64000)",
	"mmap_size(536870912)",
	"temp_store(MEMORY)",
	"wal_autocheckpoint(25000)",
	"journal_size_limit(134217728)",
}

func pragmaName(p string) string {
	name, _, _ := strings.Cut(p, "(")
	name, _, _ = strings.Cut(name, "=")
	return strings.TrimSpace(strings.ToLower(name))
}

func BuildDSN(dbPath string) string {
	basePath, queryStr, hasQuery := strings.Cut(dbPath, "?")
	q := url.Values{}
	userPragmas := make(map[string]struct{})
	if hasQuery {
		if parsed, err := url.ParseQuery(queryStr); err == nil {
			q = parsed
			for _, p := range q["_pragma"] {
				userPragmas[pragmaName(p)] = struct{}{}
			}
		}
	}
	for _, p := range defaultPragmas {
		if _, exists := userPragmas[pragmaName(p)]; !exists {
			q.Add("_pragma", p)
		}
	}
	return basePath + "?" + q.Encode()
}

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
		db, err := sql.Open("sqlite", BuildDSN(dbPath))
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

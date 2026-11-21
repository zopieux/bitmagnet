package app

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appcli "github.com/bitmagnet-io/bitmagnet/internal/app/cli"
	"github.com/bitmagnet-io/bitmagnet/internal/app/cli/hooks"
	"github.com/bitmagnet-io/bitmagnet/internal/app/cmd/workercmd"
	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/lazy"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/bitmagnet-io/bitmagnet/internal/worker"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	goose "github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

func newTestCLIApp(
	dbPath string,
	args []string,
	extra ...fx.Option,
) *fx.App {
	opts := []fx.Option{
		fx.NopLogger,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
		fx.StopTimeout(15 * time.Second),
		fx.Supply(
			zap.NewNop(),
			zap.NewNop().Sugar(),
			sqlite.Config{
				DSN:                dbPath,
				WriteInterval:      time.Minute,
				CheckpointInterval: 5 * time.Minute,
			},
			fx.Annotate(args, fx.ResultTags(`name:"cli_args"`)),
		),
		fx.Provide(
			sqlite.New,
			blocking.New,
			hooks.New,
			worker.NewRegistry,
			workercmd.New,
			appcli.New,
		),
		fx.Invoke(func(
			_ hooks.AttachedHooks,
			_ *cli.App,
		) {
		}),
	}
	opts = append(opts, extra...)
	return fx.New(opts...)
}

func TestGracefulShutdownOrder(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bitmagnet.db")

	var persistedHash, blockedHash protocol.ID
	copy(persistedHash[:], "persisted-on-stop!!!")
	copy(blockedHash[:], "blocked-before-stop!")

	started := make(chan struct{})
	var (
		mu               sync.Mutex
		stopOrder        []string
		workerStopCtxErr error
		workerFlushErr   error
	)

	probeHook := fx.Annotate(
		func() fx.Hook {
			return fx.Hook{
				OnStop: func(context.Context) error {
					mu.Lock()
					stopOrder = append(stopOrder, "app_hook")
					mu.Unlock()
					return nil
				},
			}
		},
		fx.ResultTags(`group:"app_hooks"`),
	)

	testWorker := fx.Annotate(
		func(db lazy.Lazy[*sql.DB], bm lazy.Lazy[blocking.Manager]) worker.Worker {
			var sqlDB *sql.DB
			var mgr blocking.Manager
			return worker.NewWorker("test_flusher", fx.Hook{
				OnStart: func(ctx context.Context) error {
					var err error
					sqlDB, err = db.Get()
					if err != nil {
						return err
					}
					provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, migrationssql.FS)
					if err != nil {
						return err
					}
					if _, err := provider.Up(ctx); err != nil {
						return err
					}
					mgr, err = bm.Get()
					if err != nil {
						return err
					}
					mgr.BlockUnpersisted([]protocol.ID{blockedHash})
					close(started)
					return nil
				},
				OnStop: func(ctx context.Context) error {
					mu.Lock()
					workerStopCtxErr = ctx.Err()
					stopOrder = append(stopOrder, "worker")
					mu.Unlock()

					tx, err := sqlDB.BeginTx(ctx, nil)
					if err != nil {
						workerFlushErr = err
						return err
					}
					if _, err := tx.ExecContext(ctx,
						`INSERT INTO torrents (info_hash, name) VALUES (?, ?)`,
						persistedHash[:],
						"flushed-on-stop",
					); err != nil {
						_ = tx.Rollback()
						workerFlushErr = err
						return err
					}
					onCommit, err := mgr.PrepareFlushTx(ctx, tx)
					if err != nil {
						_ = tx.Rollback()
						workerFlushErr = err
						return err
					}
					if err := tx.Commit(); err != nil {
						workerFlushErr = err
						return err
					}
					onCommit()
					return nil
				},
			})
		},
		fx.ResultTags(`group:"workers"`),
	)

	fxApp := newTestCLIApp(
		dbPath,
		[]string{"bitmagnet", "worker", "run", "--keys", "test_flusher"},
		fx.Provide(probeHook, testWorker),
	)
	require.NoError(t, fxApp.Err())

	startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startCancel()
	require.NoError(t, fxApp.Start(startCtx))

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for test_flusher worker to start")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, fxApp.Stop(stopCtx))

	assert.NoError(t, workerStopCtxErr, "worker OnStop must receive a non-canceled context")
	assert.NoError(t, workerFlushErr)
	assert.Equal(t, []string{"worker", "app_hook"}, stopOrder)

	if info, err := os.Stat(dbPath + "-wal"); err == nil {
		assert.Zero(t, info.Size(), "WAL should be truncated on shutdown")
	} else {
		assert.True(t, os.IsNotExist(err), "unexpected stat error on WAL: %v", err)
	}

	verifyDB, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer verifyDB.Close()

	var torrentCount, chunkCount, bloomCount int
	require.NoError(t, verifyDB.QueryRow(`SELECT COUNT(*) FROM torrents WHERE info_hash = ?`, persistedHash[:]).Scan(&torrentCount))
	assert.Equal(t, 1, torrentCount, "torrent flushed during worker OnStop must be persisted")

	require.NoError(t, verifyDB.QueryRow(`SELECT COUNT(*) FROM blocked_hash_chunks`).Scan(&chunkCount))
	assert.Equal(t, 0, chunkCount, "blocking.Compact must clear delta chunks after worker flush and before sqlite close")

	require.NoError(t, verifyDB.QueryRow(`SELECT COUNT(*) FROM bloom_filters WHERE key = 'blocked_torrents'`).Scan(&bloomCount))
	assert.Equal(t, 1, bloomCount, "blocking.Compact must persist bloom filter before sqlite close")
}

func TestShortLivedCLICommandExitsCleanly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bitmagnet.db")

	fxApp := newTestCLIApp(dbPath, []string{"bitmagnet", "worker", "list"})
	require.NoError(t, fxApp.Err())

	done := fxApp.Done()

	startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startCancel()
	require.NoError(t, fxApp.Start(startCtx))

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for short-lived CLI command shutdown")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	require.NoError(t, fxApp.Stop(stopCtx))
}

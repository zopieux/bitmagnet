package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/archive"
	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/classify"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	_ "github.com/glebarez/go-sqlite"
	"github.com/go-playground/validator/v10"
	"github.com/go-viper/mapstructure/v2"
	"github.com/iancoleman/strcase"
	"gopkg.in/yaml.v3"
)

func classifyActions(config classify.Config, torrents []model.Torrent, workers int) []classify.Action {
	actions := make([]classify.Action, len(torrents))

	if workers <= 1 || len(torrents) <= 1 {
		for i := range torrents {
			actions[i] = config.ActionFor(torrents[i])
		}

		return actions
	}

	if workers > len(torrents) {
		workers = len(torrents)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup

	wg.Add(workers)

	for range workers {
		go func() {
			defer wg.Done()

			for i := range jobs {
				actions[i] = config.ActionFor(torrents[i])
			}
		}()
	}

	for i := range torrents {
		jobs <- i
	}

	close(jobs)
	wg.Wait()

	return actions
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func loadConfig(path string) (classify.Config, error) {
	config := classify.NewDefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return config, err
	}

	if nested, ok := raw["classify"]; ok {
		nestedMap, ok := nested.(map[string]interface{})
		if !ok {
			return config, errors.New("classify config must be a mapping")
		}
		raw = nestedMap
	}

	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result: &config,
		MatchName: func(mapKey, fieldName string) bool {
			return mapKey == strcase.ToSnake(fieldName)
		},
	})
	if err != nil {
		return config, err
	}

	if err := decoder.Decode(raw); err != nil {
		return config, err
	}

	if err := validator.New().Struct(config); err != nil {
		return config, err
	}

	return config, nil
}

func resolveDBPaths(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}

	mainPath := path
	archivePath := sqlite.ArchiveDBPath(path)

	if info.IsDir() {
		mainPath = filepath.Join(path, "bitmagnet.db")
		archivePath = filepath.Join(path, "archive.db")
	}

	if _, err := os.Stat(mainPath); err != nil {
		return "", "", err
	}

	return mainPath, archivePath, nil
}

func fetchBatch(
	ctx context.Context,
	db *sql.DB,
	after protocol.ID,
	hasAfter bool,
	limit int,
) ([]model.Torrent, error) {
	query := `SELECT info_hash, name FROM torrents`
	args := make([]any, 0, 2)

	if hasAfter {
		query += ` WHERE info_hash > ?`
		args = append(args, after[:])
	}

	query += ` ORDER BY info_hash LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	torrents := make([]model.Torrent, 0, limit)
	hashes := make([]protocol.ID, 0, limit)

	for rows.Next() {
		var (
			raw  []byte
			name string
		)
		if err := rows.Scan(&raw, &name); err != nil {
			return nil, err
		}

		var infoHash protocol.ID
		copy(infoHash[:], raw)

		torrents = append(torrents, model.Torrent{
			InfoHash: infoHash,
			Name:     name,
		})
		hashes = append(hashes, infoHash)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(hashes) == 0 {
		return torrents, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(hashes)), ",")
	fileArgs := make([]any, len(hashes))
	for i, hash := range hashes {
		fileArgs[i] = hash[:]
	}

	fileRows, err := db.QueryContext(
		ctx,
		`SELECT info_hash, path, extension, size FROM torrent_files WHERE info_hash IN (`+placeholders+`)`,
		fileArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer fileRows.Close()

	filesByHash := make(map[protocol.ID][]model.TorrentFile, len(hashes))

	for fileRows.Next() {
		var (
			raw       []byte
			path      string
			extension model.NullString
			size      uint
		)
		if err := fileRows.Scan(&raw, &path, &extension, &size); err != nil {
			return nil, err
		}

		var infoHash protocol.ID
		copy(infoHash[:], raw)

		filesByHash[infoHash] = append(filesByHash[infoHash], model.TorrentFile{
			InfoHash:  infoHash,
			Path:      path,
			Extension: extension,
			Size:      size,
		})
	}

	if err := fileRows.Err(); err != nil {
		return nil, err
	}

	for i := range torrents {
		torrents[i].Files = filesByHash[torrents[i].InfoHash]
	}

	return torrents, nil
}

func needsArchive(config classify.Config) bool {
	return config.BannedAction == classify.BannedActionArchive ||
		config.PornAction == classify.PornActionArchive ||
		config.CyrillicAction == classify.ScriptActionArchive ||
		config.KoreanAction == classify.ScriptActionArchive ||
		config.ChineseAction == classify.ScriptActionArchive ||
		config.JapaneseAction == classify.ScriptActionArchive ||
		config.ThaiAction == classify.ScriptActionArchive
}

func main() {
	configPath := flag.String("config", "", "path to classifier config YAML")
	batchSize := flag.Int("batch-size", 500, "number of torrents to process per batch")
	workers := flag.Int("workers", runtime.GOMAXPROCS(0), "number of classifier workers")
	flushSize := flag.Int("flush-size", 100_000, "number of matching torrents to accumulate before flushing deletes/archives")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -config <config.yml> <dir-with-bitmagnet.db-and-archive.db>\n\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Applies classify actions (keep/delete/archive) to an existing database.\n\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *configPath == "" {
		_, _ = fmt.Fprintln(os.Stderr, "error: -config is required")
		flag.Usage()
		os.Exit(1)
	}

	if *batchSize <= 0 {
		fatal(errors.New("error: -batch-size must be greater than 0"))
	}

	if *workers <= 0 {
		fatal(errors.New("error: -workers must be greater than 0"))
	}

	if *flushSize <= 0 {
		fatal(errors.New("error: -flush-size must be greater than 0"))
	}

	if flag.NArg() != 1 {
		_, _ = fmt.Fprintln(os.Stderr, "error: exactly one database directory must be specified")
		flag.Usage()
		os.Exit(1)
	}

	config, err := loadConfig(*configPath)
	if err != nil {
		fatal(fmt.Errorf("failed to load config: %w", err))
	}

	mainPath, archivePath, err := resolveDBPaths(flag.Arg(0))
	if err != nil {
		fatal(err)
	}

	ctx := context.Background()

	db, err := sql.Open("sqlite", sqlite.BuildDSN(mainPath))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if needsArchive(config) {
		archiveDB, err := sql.Open("sqlite", sqlite.BuildDSN(archivePath))
		if err != nil {
			fatal(err)
		}
		if err := archive.Up(ctx, archiveDB); err != nil {
			_ = archiveDB.Close()
			fatal(fmt.Errorf("failed to migrate archive db: %w", err))
		}
		_ = archiveDB.Close()
	}

	manager := blocking.NewManager(db, archivePath, time.Second)

	start := time.Now()
	processed := 0
	deleted := 0
	archived := 0
	kept := 0

	var (
		after    protocol.ID
		hasAfter bool
	)

	var pendingDeletes, pendingArchives []protocol.ID

	flushDeletes := func() {
		if len(pendingDeletes) == 0 {
			return
		}

		if err := manager.Block(ctx, pendingDeletes, true); err != nil {
			fatal(fmt.Errorf("delete batch failed: %w", err))
		}

		pendingDeletes = pendingDeletes[:0]
	}

	flushArchives := func() {
		if len(pendingArchives) == 0 {
			return
		}

		if err := manager.Archive(ctx, pendingArchives); err != nil {
			fatal(fmt.Errorf("archive batch failed: %w", err))
		}

		pendingArchives = pendingArchives[:0]
	}

	for {
		torrents, err := fetchBatch(ctx, db, after, hasAfter, *batchSize)
		if err != nil {
			fatal(err)
		}
		if len(torrents) == 0 {
			break
		}

		after = torrents[len(torrents)-1].InfoHash
		hasAfter = true

		actions := classifyActions(config, torrents, *workers)

		for i, torrent := range torrents {
			switch actions[i] {
			case classify.ActionDelete:
				pendingDeletes = append(pendingDeletes, torrent.InfoHash)
				deleted++
			case classify.ActionArchive:
				pendingArchives = append(pendingArchives, torrent.InfoHash)
				archived++
			default:
				kept++
			}
		}

		if len(pendingArchives) >= *flushSize {
			flushArchives()
		}

		if len(pendingDeletes) >= *flushSize {
			flushDeletes()
		}

		processed += len(torrents)
		fmt.Printf(
			"Processed: %d torrents (deleted %d, archived %d, kept %d) (%.1fs)...\n",
			processed, deleted, archived, kept, time.Since(start).Seconds(),
		)
	}

	flushArchives()
	flushDeletes()

	fmt.Printf(
		"Done: %d torrents processed in %s (deleted %d, archived %d, kept %d)\n",
		processed, time.Since(start).Round(time.Millisecond), deleted, archived, kept,
	)
}

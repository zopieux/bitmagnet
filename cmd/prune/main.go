package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/archive"
	"github.com/bitmagnet-io/bitmagnet/internal/blocking"
	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	_ "github.com/glebarez/go-sqlite"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSliceFlag) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func buildTagQuery(tags []string) (string, []any) {
	if len(tags) == 1 {
		return `SELECT info_hash FROM torrent_tags WHERE name = ? LIMIT ?`, []any{tags[0]}
	}
	var b strings.Builder
	b.WriteString("SELECT t0.info_hash FROM torrent_tags t0\n")
	for i := 1; i < len(tags); i++ {
		fmt.Fprintf(&b, "JOIN torrent_tags t%d ON t%d.info_hash = t0.info_hash AND t%d.name = ?\n", i, i, i)
	}
	b.WriteString("WHERE t0.name = ?\nLIMIT ?")
	args := make([]any, len(tags))
	for i := 1; i < len(tags); i++ {
		args[i-1] = tags[i]
	}
	args[len(tags)-1] = tags[0]
	return b.String(), args
}

func fetchNextBatch(ctx context.Context, db *sql.DB, query string, baseArgs []any, limit int) ([]protocol.ID, error) {
	args := append(slices.Clone(baseArgs), limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes []protocol.ID
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var id protocol.ID
		copy(id[:], raw)
		hashes = append(hashes, id)
	}
	return hashes, rows.Err()
}

func main() {
	var tags stringSliceFlag
	flag.Var(&tags, "tag", "tag to filter by (can be repeated for AND matching)")
	deleteFlag := flag.Bool("delete", false, "hard delete matching torrents")
	archiveFlag := flag.Bool("archive", false, "archive matching torrents to archive.db")
	batchSize := flag.Int("batch-size", 500, "number of torrents to process per batch")
	dsn := flag.String("dsn", os.Getenv("DB_DSN"), "database path (defaults to the DB_DSN env var, then ./bitmagnet.db)")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --tag <sometag> --delete [--batch-size N]\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "  %s --tag <sometag> --archive [--batch-size N]\n\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "When multiple --tag flags are given, torrents are matched by AND.\n\nOptions:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if (*deleteFlag && *archiveFlag) || (!*deleteFlag && !*archiveFlag) {
		_, _ = fmt.Fprintln(os.Stderr, "error: must specify exactly one of --delete or --archive")
		flag.Usage()
		os.Exit(1)
	}

	if *batchSize <= 0 {
		fatal(fmt.Errorf("error: --batch-size must be greater than 0"))
	}

	var cleanTags []string
	seen := make(map[string]struct{})
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			cleanTags = append(cleanTags, t)
		}
	}

	if len(cleanTags) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "error: at least one non-empty --tag must be specified")
		flag.Usage()
		os.Exit(1)
	}

	if *dsn == "" {
		*dsn = "bitmagnet.db"
	}

	ctx := context.Background()

	db, err := sql.Open("sqlite", sqlite.BuildDSN(*dsn))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	archivePath := sqlite.ArchiveDBPath(*dsn)

	if *archiveFlag {
		adb, err := sql.Open("sqlite", sqlite.BuildDSN(archivePath))
		if err != nil {
			fatal(err)
		}
		if err := archive.Up(ctx, adb); err != nil {
			_ = adb.Close()
			fatal(fmt.Errorf("failed to migrate archive db: %w", err))
		}
		_ = adb.Close()
	}

	query, baseArgs := buildTagQuery(cleanTags)
	action := "Deleting"
	if *archiveFlag {
		action = "Archiving"
	}

	fmt.Printf("%s torrents matching tag(s): %s (batch size %d)...\n", action, strings.Join(cleanTags, " AND "), *batchSize)

	mgr := blocking.NewManager(db, archivePath, time.Second)
	start := time.Now()
	processed := 0
	var firstHashInLastBatch protocol.ID

	for {
		hashes, err := fetchNextBatch(ctx, db, query, baseArgs, *batchSize)
		if err != nil {
			fatal(err)
		}
		if len(hashes) == 0 {
			break
		}
		if processed > 0 && hashes[0] == firstHashInLastBatch {
			fatal(errors.New("aborting: matched torrents were not removed after operation; check foreign keys"))
		}
		firstHashInLastBatch = hashes[0]

		if *archiveFlag {
			if err := mgr.Archive(ctx, hashes); err != nil {
				fatal(fmt.Errorf("archive batch failed: %w", err))
			}
		} else {
			if err := mgr.Block(ctx, hashes, true); err != nil {
				fatal(fmt.Errorf("delete batch failed: %w", err))
			}
		}

		processed += len(hashes)
		fmt.Printf("%s: %d torrents (%.1fs)...\n", action, processed, time.Since(start).Seconds())
	}

	if processed == 0 {
		fmt.Printf("No torrents found matching tag(s): %s\n", strings.Join(cleanTags, " AND "))
		return
	}

	fmt.Printf("%s: done, %d torrents in %s\n", action, processed, time.Since(start).Round(time.Millisecond))
}

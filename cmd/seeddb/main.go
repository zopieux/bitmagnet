// Command seeddb fills a bitmagnet SQLite database with generated torrents
// (torrents, contents, files, sources, tags) for manual load/UI testing.
// It is a standalone tool: nothing in the app references it.
//
// Usage:
//
//	DB_DSN=/path/to.db go run ./cmd/seeddb [-count 200000] [-seed seeddb]
//
// The database is created and migrated (same goose migrations as the app)
// if needed. Rows are appended; re-running with the same seed and count
// will fail on duplicate info hashes, which is intentional.
package main

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/database/sqlite"
	migrationssql "github.com/bitmagnet-io/bitmagnet/migrations"
	_ "github.com/glebarez/go-sqlite"
	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
)

var (
	adjectives = []string{"Ancient", "Silent", "Golden", "Crimson", "Electric", "Hidden", "Broken", "Velvet", "Iron", "Scarlet", "Quiet", "Burning", "Frozen", "Hollow", "Radiant"}
	subjects   = []string{"Ocean", "Mountain", "Desert", "Forest", "River", "Dragon", "Turtle", "Fox", "Wolf", "Bear", "City", "Garden", "Harbor", "Valley", "Storm"}
	works      = []string{"Adventures", "Chronicles", "Journey", "Mystery", "Documentary", "Movie", "Tale", "Saga", "Odyssey", "Requiem"}
	qualities  = []string{"720p", "1080p", "2160p"}
	sources    = []string{"BluRay", "WEB-DL", "HDTV", "WEBRip"}
	resByQual  = map[string]string{"720p": "V720p", "1080p": "V1080p", "2160p": "V2160p"}
	srcByName  = map[string]string{"BluRay": "BluRay", "WEB-DL": "WEBDL", "HDTV": "TV", "WEBRip": "WEBRip"}
	tagPool    = []string{"4k", "hdr", "remux", "flac", "lossless", "dubbed", "subbed", "complete-pack", "french", "german", "x265", "proper"}
)

type contentKind struct {
	typ    string
	weight int
}

var kinds = []contentKind{
	{"movie", 45},
	{"tv_show", 20},
	{"music", 10},
	{"ebook", 8},
	{"audiobook", 5},
	{"comic", 4},
	{"game", 4},
	{"software", 4},
}

func pickKind(rng *rand.Rand) string {
	total := 0
	for _, k := range kinds {
		total += k.weight
	}
	n := rng.Intn(total)
	for _, k := range kinds {
		n -= k.weight
		if n < 0 {
			return k.typ
		}
	}
	return "movie"
}

func main() {
	count := flag.Int("count", 200000, "number of torrents to insert")
	seed := flag.String("seed", "seeddb", "generation seed (determines titles and info hashes)")
	dsn := flag.String("dsn", os.Getenv("DB_DSN"), "database path (defaults to the DB_DSN env var, then ./bitmagnet.db)")
	flag.Parse()

	if *dsn == "" {
		*dsn = "bitmagnet.db"
	}

	db, err := sql.Open("sqlite", sqlite.BuildDSN(*dsn))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	goose.SetBaseFS(migrationssql.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		fatal(err)
	}
	if err := goose.UpContext(context.Background(), db, "."); err != nil {
		fatal(fmt.Errorf("migrations: %w", err))
	}

	if _, err := db.Exec(`INSERT OR IGNORE INTO torrent_sources (key, name) VALUES ('dht', 'DHT')`); err != nil {
		fatal(err)
	}

	seedHash := sha1.Sum([]byte(*seed))
	seedBase := int64(seedHash[0])<<56 | int64(seedHash[1])<<48 | int64(seedHash[2])<<40 | int64(seedHash[3])<<32 |
		int64(seedHash[4])<<24 | int64(seedHash[5])<<16 | int64(seedHash[6])<<8 | int64(seedHash[7])

	now := time.Now().UTC()
	start := time.Now()
	const batchSize = 1000
	inserted := 0

	for base := 0; base < *count; base += batchSize {
		tx, err := db.Begin()
		if err != nil {
			fatal(err)
		}
		if err := insertBatch(tx, seedBase, base, min(batchSize, *count-base), now); err != nil {
			_ = tx.Rollback()
			fatal(err)
		}
		if err := tx.Commit(); err != nil {
			fatal(err)
		}
		inserted += min(batchSize, *count-base)
		if inserted%20000 == 0 || inserted == *count {
			fmt.Printf("seeddb: %d/%d torrents (%.0fs)\n", inserted, *count, time.Since(start).Seconds())
		}
	}

	fmt.Printf("seeddb: done, %d torrents in %s -> %s\n", *count, time.Since(start).Round(time.Second), *dsn)
}

func insertBatch(tx *sql.Tx, seedBase int64, base, n int, now time.Time) error {
	torrents, err := tx.Prepare(`INSERT INTO torrents (info_hash, name, size, private, files_status, files_count, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer torrents.Close()

	contents, err := tx.Prepare(`INSERT INTO torrent_contents (id, info_hash, content_type, languages, episodes, video_resolution, video_source, video_codec, seeders, leechers, published_at, size, files_count, created_at, updated_at) VALUES (?, ?, ?, '["en"]', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer contents.Close()

	srcRows, err := tx.Prepare(`INSERT INTO torrents_torrent_sources (source, info_hash, seeders, leechers, published_at, created_at, updated_at) VALUES ('dht', ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer srcRows.Close()

	files, err := tx.Prepare(`INSERT INTO torrent_files (info_hash, "index", path, extension, size, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer files.Close()

	tags, err := tx.Prepare(`INSERT OR IGNORE INTO torrent_tags (info_hash, name, created_at, updated_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer tags.Close()

	for i := 0; i < n; i++ {
		idx := base + i
		rng := rand.New(rand.NewSource(seedBase + int64(idx)))
		kind := pickKind(rng)
		title, filesCount, totalSize := generateTitleAndSize(rng, kind)

		infoHash := sha1.Sum([]byte(fmt.Sprintf("seeddb:%d:%d", seedBase, idx)))
		created := now.Add(-time.Duration(rng.Intn(60*24*30)) * time.Minute) // spread over the last ~30 days
		published := created.Add(-time.Duration(rng.Intn(72)) * time.Hour)
		seeders := rng.Intn(50)
		if rng.Intn(4) == 0 {
			seeders = rng.Intn(2000)
		}
		leechers := rng.Intn(80)

		filesStatus := "single"
		if filesCount > 1 {
			filesStatus = "multi"
		}

		if _, err := torrents.Exec(infoHash[:], title, totalSize, filesStatus, filesCount, created, created); err != nil {
			return fmt.Errorf("torrent %d: %w", idx, err)
		}

		episodes := "{}"
		var res, src, codec any
		if kind == "tv_show" {
			episodes = fmt.Sprintf(`{"%d":{"%d":{}}}`, 1+rng.Intn(5), 1+rng.Intn(12))
		}
		if kind == "movie" || kind == "tv_show" {
			q := qualities[rng.Intn(len(qualities))]
			res = resByQual[q]
			src = srcByName[sources[rng.Intn(len(sources))]]
			if rng.Intn(2) == 0 {
				codec = "x265"
			} else {
				codec = "x264"
			}
		}
		if _, err := contents.Exec(uuid.NewString(), infoHash[:], kind, episodes, res, src, codec, seeders, leechers, published, totalSize, filesCount, created, created); err != nil {
			return fmt.Errorf("content %d: %w", idx, err)
		}

		if _, err := srcRows.Exec(infoHash[:], seeders, leechers, published, created, created); err != nil {
			return fmt.Errorf("source %d: %w", idx, err)
		}

		// Split the total size across the files.
		remaining := totalSize
		for f := 0; f < filesCount; f++ {
			var path, ext string
			size := remaining
			if f < filesCount-1 {
				size = remaining / int64(filesCount-f)
				remaining -= size
			}
			switch kind {
			case "music":
				ext = "flac"
				path = fmt.Sprintf("%s/%02d - Track %02d.flac", title, f+1, f+1)
			case "audiobook":
				ext = "mp3"
				path = fmt.Sprintf("%s/Chapter %02d.mp3", title, f+1)
			case "movie", "tv_show":
				ext = "mkv"
				if filesCount > 1 {
					path = fmt.Sprintf("%s/Part %d.mkv", title, f+1)
				} else {
					path = title + ".mkv"
				}
			default:
				ext = "bin"
				path = fmt.Sprintf("%s/file-%d.bin", title, f+1)
			}
			if _, err := files.Exec(infoHash[:], f, path, ext, size, created, created); err != nil {
				return fmt.Errorf("file %d/%d: %w", idx, f, err)
			}
		}

		if rng.Intn(3) == 0 {
			tag := tagPool[rng.Intn(len(tagPool))]
			if _, err := tags.Exec(infoHash[:], tag, created, created); err != nil {
				return fmt.Errorf("tag %d: %w", idx, err)
			}
		}
	}
	return nil
}

func generateTitleAndSize(rng *rand.Rand, kind string) (string, int, int64) {
	adj := adjectives[rng.Intn(len(adjectives))]
	subj := subjects[rng.Intn(len(subjects))]
	work := works[rng.Intn(len(works))]
	year := 1975 + rng.Intn(51)

	switch kind {
	case "movie":
		q := qualities[rng.Intn(len(qualities))]
		src := sources[rng.Intn(len(sources))]
		size := int64(700_000_000 + rng.Intn(18_000_000_000))
		return fmt.Sprintf("The %s %s %s %d %s %s", adj, subj, work, year, q, src), 1, size
	case "tv_show":
		return fmt.Sprintf("%s %s %s S%02dE%02d %s", adj, subj, work, 1+rng.Intn(6), 1+rng.Intn(14), qualities[rng.Intn(len(qualities))]), 1, int64(300_000_000 + rng.Intn(3_000_000_000))
	case "music":
		tracks := 8 + rng.Intn(14)
		return fmt.Sprintf("%s %s - Album %d [FLAC]", adj, subj, year), tracks, int64(tracks) * 30_000_000
	case "ebook":
		return fmt.Sprintf("%s %s %s by Author %d epub", adj, subj, work, rng.Intn(500)), 1, int64(1_000_000 + rng.Intn(20_000_000))
	case "audiobook":
		chapters := 10 + rng.Intn(20)
		return fmt.Sprintf("%s %s %s (Audiobook)", adj, subj, work), chapters, int64(chapters) * 25_000_000
	case "comic":
		return fmt.Sprintf("%s %s Vol %d CBZ", adj, subj, 1+rng.Intn(30)), 1, int64(20_000_000 + rng.Intn(400_000_000))
	case "game":
		return fmt.Sprintf("%s %s %s - FitGirl Repack", adj, subj, work), 3 + rng.Intn(10), int64(2_000_000_000 + rng.Intn(90_000_000_000))
	case "software":
		return fmt.Sprintf("%s %s Suite %d Pro", adj, subj, year), 2 + rng.Intn(5), int64(100_000_000 + rng.Intn(4_000_000_000))
	}
	return fmt.Sprintf("%s %s %s", adj, subj, work), 1, 1_000_000
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "seeddb:", err)
	os.Exit(1)
}

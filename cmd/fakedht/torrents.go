// Deterministic fake torrent generation.
package main

import (
	"crypto/sha1"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"net"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol/dht"
)

type fakeTorrent struct {
	infoHash [20]byte
	infoDict []byte
	title    string
}

// scrapeFilters builds the BEP 33 bloom filters for this torrent's fake
// swarm. The swarm composition (seeders + leechers, as peer IPs) is derived
// deterministically from the info hash, so counts are stable across runs.
// Seeders are inserted into both filters; leechers only into BFpe.
func (t fakeTorrent) scrapeFilters() (bfpe, bfsd [256]byte) {
	h := fnv.New64a()
	_, _ = h.Write([]byte("fakedht-scrape"))
	_, _ = h.Write(t.infoHash[:])
	rng := rand.New(rand.NewSource(int64(h.Sum64())))

	seeders := rng.Intn(500)
	leechers := rng.Intn(900)

	var pe, sd dht.ScrapeBloomFilter
	for i := 0; i < seeders; i++ {
		ip := fakePeerIP(rng)
		sd.AddIP(ip)
		pe.AddIP(ip)
	}

	for i := 0; i < leechers; i++ {
		pe.AddIP(fakePeerIP(rng))
	}

	return [256]byte(pe), [256]byte(sd)
}

func fakePeerIP(rng *rand.Rand) net.IP {
	return net.IPv4(
		byte(1+rng.Intn(223)),
		byte(rng.Intn(256)),
		byte(rng.Intn(256)),
		byte(1+rng.Intn(254)),
	)
}

var titleA = []string{"The", "A", "An"}
var titleB = []string{"Quick", "Slow", "Brown", "Green", "Red", "Silent", "Loud", "Ancient"}
var titleC = []string{"Fox", "Turtle", "Dragon", "River", "Mountain", "Ocean", "Forest", "Desert"}
var titleD = []string{"Documentary", "Movie", "Chronicles", "Adventures", "Mystery", "Journey"}
var titleE = []string{"2021", "2022", "2023", "2024", "2025"}
var titleF = []string{"720p", "1080p", "2160p"}
var titleG = []string{"BluRay", "WEB-DL", "HDTV"}

func seedFromString(s string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return int64(h.Sum64())
}

func generateTitle(rng *rand.Rand) string {
	return fmt.Sprintf("%s %s %s %s %s %s %s",
		titleA[rng.Intn(len(titleA))],
		titleB[rng.Intn(len(titleB))],
		titleC[rng.Intn(len(titleC))],
		titleD[rng.Intn(len(titleD))],
		titleE[rng.Intn(len(titleE))],
		titleF[rng.Intn(len(titleF))],
		titleG[rng.Intn(len(titleG))],
	)
}

const (
	// maxTotalSize caps a torrent's total size at 10 TiB.
	maxTotalSize = 10 << 40
	// maxFiles caps a torrent's file count.
	maxFiles = 10_000
)

// pareto draws from a Pareto (power-law) distribution with the given
// minimum and shape: most draws land near min, a few are enormous.
func pareto(rng *rand.Rand, min float64, alpha float64) float64 {
	return min * math.Pow(1-rng.Float64(), -1/alpha)
}

// fileCount picks a torrent's file count from a long-tailed mixture:
// mostly single-file, multi-file counts are Pareto, and a rare "monster"
// regime produces the few-thousand-file torrents a real DHT sample has.
func fileCount(rng *rand.Rand) int {
	switch r := rng.Float64(); {
	case r < 0.003:
		// Monster: 5k-10k files.
		return 5_000 + rng.Intn(5_001)
	case r < 0.60:
		return 1
	default:
		n := int(pareto(rng, 2, 1.05))
		if n > maxFiles {
			n = maxFiles
		}
		return n
	}
}

// fileSizes picks per-file sizes (Pareto; monsters draw from a larger
// minimum so their totals reach the TB range) and scales everything down
// proportionally if the total exceeds the 10 TiB cap.
func fileSizes(rng *rand.Rand, n int, monster bool) []int64 {
	minMB := 1.0
	if monster {
		minMB = 50
	}
	sizes := make([]int64, n)
	var total int64
	for i := range sizes {
		s := int64(pareto(rng, minMB*(1<<20), 1.15))
		if s > 2<<40 {
			s = 2 << 40
		}
		sizes[i] = s
		total += s
	}
	if total > maxTotalSize {
		scale := float64(maxTotalSize) / float64(total)
		total = 0
		for i := range sizes {
			sizes[i] = int64(float64(sizes[i]) * scale)
			if sizes[i] < 1 {
				sizes[i] = 1
			}
			total += sizes[i]
		}
	}
	return sizes
}

// pieceLength picks a realistic piece length for the total size: a power
// of two giving at most ~2048 pieces, clamped to [256 KiB, 16 MiB].
func pieceLength(total int64) int64 {
	pl := int64(262144)
	for pl < 16<<20 && total/pl > 2048 {
		pl *= 2
	}
	return pl
}

func generateTorrents(seed string, count int) []fakeTorrent {
	rng := rand.New(rand.NewSource(seedFromString(seed)))
	out := make([]fakeTorrent, 0, count)
	for i := 0; i < count; i++ {
		title := generateTitle(rng)
		// 20 bytes of fake piece hashes (single piece).
		pieces := make([]byte, 20)
		_, _ = rng.Read(pieces)

		nFiles := fileCount(rng)
		monster := nFiles >= 5_000
		var dict map[string]interface{}
		if nFiles == 1 {
			// Single-file torrent.
			size := int64(pareto(rng, 50*(1<<20), 1.15))
			if size > maxTotalSize {
				size = maxTotalSize
			}
			dict = map[string]interface{}{
				"name":         title + ".mkv",
				"length":       size,
				"piece length": pieceLength(size),
				"pieces":       pieces,
			}
		} else {
			// Multi-file torrent.
			sizes := fileSizes(rng, nFiles, monster)
			var total int64
			for _, s := range sizes {
				total += s
			}
			files := make([]interface{}, 0, nFiles)
			for f, s := range sizes {
				files = append(files, map[string]interface{}{
					"length": s,
					"path":   []interface{}{fmt.Sprintf("%s.part%d.mkv", title, f+1)},
				})
			}
			dict = map[string]interface{}{
				"name":         title,
				"files":        files,
				"piece length": pieceLength(total),
				"pieces":       pieces,
			}
		}
		infoDict := bencode(dict)
		infoHash := sha1.Sum(infoDict)
		out = append(out, fakeTorrent{infoHash: infoHash, infoDict: infoDict, title: title})
	}
	return out
}

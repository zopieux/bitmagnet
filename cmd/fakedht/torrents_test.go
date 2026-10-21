package main

import (
	"math/rand"
	"sort"
	"testing"
)

// The generated population must be long-tailed: mostly small torrents,
// a few monsters with thousands of files and TB-scale totals, and nothing
// above the 10 TiB / 10k-file caps.
func TestGenerateTorrentsLongTail(t *testing.T) {
	rng := rand.New(rand.NewSource(seedFromString("fakedht")))

	const n = 2000
	singles := 0
	mostFiles := 0
	monsters := 0
	totals := make([]int64, 0, n)

	for i := 0; i < n; i++ {
		fc := fileCount(rng)
		if fc == 1 {
			singles++
			size := int64(pareto(rng, 50*(1<<20), 1.15))
			totals = append(totals, size)
			continue
		}
		if fc > mostFiles {
			mostFiles = fc
		}
		if fc >= 5_000 {
			monsters++
		}
		var total int64
		for _, s := range fileSizes(rng, fc, fc >= 5_000) {
			total += s
		}
		if total > maxTotalSize {
			t.Fatalf("total %d exceeds cap %d", total, maxTotalSize)
		}
		totals = append(totals, total)
	}

	if frac := float64(singles) / n; frac < 0.5 || frac > 0.7 {
		t.Fatalf("single-file fraction %.2f outside [0.5, 0.7]", frac)
	}
	if monsters == 0 {
		t.Fatal("no monster torrents (>=5000 files) in 2000 draws")
	}
	if mostFiles > maxFiles {
		t.Fatalf("file count %d exceeds cap", mostFiles)
	}

	sort.Slice(totals, func(a, b int) bool { return totals[a] < totals[b] })
	median := totals[n/2]
	if median > 500<<20 {
		t.Fatalf("median total size %d too large: population is not mostly small", median)
	}
	if max := totals[n-1]; max < 1<<40 {
		t.Fatalf("largest total %d: expected at least one TB-scale torrent", max)
	}
}

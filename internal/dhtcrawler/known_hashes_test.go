package dhtcrawler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnownHashesLoadAndLookup(t *testing.T) {
	_, gormDB := setupCrawlerTestDB(t)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)

	summaryBytes, err := model.ContentSummary{TotalFiles: 2, TotalSize: 200}.Value()
	require.NoError(t, err)

	hSingle := protocol.ID{1}
	hMultiDone := protocol.ID{2}
	hMultiNoSummary := protocol.ID{3}
	hOverAbove := protocol.ID{4}
	hOverBelow := protocol.ID{5}
	hOverNoSummary := protocol.ID{6}
	hNoInfo := protocol.ID{7}
	hZero := protocol.ID{}

	insertTorrent := func(h protocol.ID, name string, status model.FilesStatus, count *uint, summary any) {
		_, execErr := sqlDB.Exec(
			"INSERT INTO torrents (info_hash, name, size, files_status, files_count, files_summary) VALUES (?, ?, 1000, ?, ?, ?)",
			h[:], name, status, count, summary,
		)
		require.NoError(t, execErr)
	}

	c2 := uint(2)
	c50 := uint(50)
	c200 := uint(200)

	insertTorrent(hSingle, "single", model.FilesStatusSingle, nil, nil)
	insertTorrent(hMultiDone, "multi-done", model.FilesStatusMulti, &c2, summaryBytes)
	insertTorrent(hMultiNoSummary, "multi-missing", model.FilesStatusMulti, &c2, nil)
	insertTorrent(hOverAbove, "over-above", model.FilesStatusOverThreshold, &c200, summaryBytes)
	insertTorrent(hOverBelow, "over-below", model.FilesStatusOverThreshold, &c50, summaryBytes)
	insertTorrent(hOverNoSummary, "over-nosummary", model.FilesStatusOverThreshold, &c200, nil)
	insertTorrent(hNoInfo, "no-info", model.FilesStatusNoInfo, nil, nil)
	insertTorrent(hZero, "all-zero", model.FilesStatusSingle, nil, nil)

	scrapeDate := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	_, err = sqlDB.Exec(
		"INSERT INTO torrents_torrent_sources (source, info_hash, seeders, leechers, updated_at) VALUES ('dht', ?, 5, 2, ?)",
		hSingle[:], scrapeDate.Format("2006-01-02 15:04:05.999999999-07:00"),
	)
	require.NoError(t, err)
	_, err = sqlDB.Exec(
		"INSERT INTO torrents_torrent_sources (source, info_hash, seeders, leechers, updated_at) VALUES ('dht', ?, NULL, NULL, ?)",
		hMultiDone[:], scrapeDate.Format("2006-01-02 15:04:05"),
	)
	require.NoError(t, err)
	_, err = sqlDB.Exec(
		"INSERT INTO torrents_torrent_sources (source, info_hash, seeders, leechers, updated_at) VALUES ('dht', ?, 3, 1, ?)",
		hOverAbove[:], scrapeDate.Format(time.RFC3339),
	)
	require.NoError(t, err)

	kh := newKnownHashes()
	require.NoError(t, kh.Load(context.Background(), sqlDB, 100))

	assert.Equal(t, 4, kh.Len())

	known, day := kh.Lookup(hSingle)
	assert.True(t, known)
	assert.Equal(t, encodeDay(scrapeDate), day)

	known, day = kh.Lookup(hMultiDone)
	assert.True(t, known)
	assert.Equal(t, uint16(0), day, "null S/L should keep day as 0")

	known, day = kh.Lookup(hOverAbove)
	assert.True(t, known)
	assert.Equal(t, encodeDay(scrapeDate), day)

	known, _ = kh.Lookup(hZero)
	assert.True(t, known, "all-zero ID must be supported")

	for _, incomplete := range []protocol.ID{hMultiNoSummary, hOverBelow, hOverNoSummary, hNoInfo} {
		known, _ = kh.Lookup(incomplete)
		assert.False(t, known, "incomplete hash %x should not be marked known", incomplete[:4])
	}

	newDay := encodeDay(scrapeDate.Add(48 * time.Hour))
	kh.UpdateScrapeDay(hMultiDone, newDay)
	known, day = kh.Lookup(hMultiDone)
	assert.True(t, known)
	assert.Equal(t, newDay, day)
}

func TestKnownHashesGrow(t *testing.T) {
	kh := newKnownHashes()
	const total = 25000 // Exceeds 70% of initial 16384 capacity.

	for i := range total {
		var id protocol.ID
		copy(id[:], fmt.Sprintf("hash-%015d", i))
		kh.MarkKnown(id, uint16((i%500)+1))
	}

	assert.Equal(t, total, kh.Len())

	for i := range total {
		var id protocol.ID
		copy(id[:], fmt.Sprintf("hash-%015d", i))
		known, day := kh.Lookup(id)
		require.True(t, known)
		require.Equal(t, uint16((i%500)+1), day)
	}
}

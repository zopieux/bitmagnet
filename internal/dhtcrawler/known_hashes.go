package dhtcrawler

import (
	"context"
	"database/sql"
	"encoding/binary"
	"strings"
	"sync"
	"time"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/bitmagnet-io/bitmagnet/internal/protocol"
)

var dayEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

type knownHashes struct {
	mu    sync.RWMutex
	keys  []uint64
	days  []uint16
	count int
}

func newKnownHashes() *knownHashes {
	const initialCap = 1 << 14
	return &knownHashes{
		keys: make([]uint64, initialCap),
		days: make([]uint16, initialCap),
	}
}

func hashKey(id protocol.ID) uint64 {
	k := mix64(binary.LittleEndian.Uint64(id[0:8])) ^
		mix64(binary.LittleEndian.Uint64(id[8:16])^0x9e3779b97f4a7c15) ^
		mix64(uint64(binary.LittleEndian.Uint32(id[16:20]))^0xc2b2ae3d27d4eb4f)
	if k == 0 {
		// 0 marks empty slots in the open-addressed table.
		return 1
	}
	return k
}

func mix64(k uint64) uint64 {
	k ^= k >> 30
	k *= 0xbf58476d1ce4e5b9
	k ^= k >> 27
	k *= 0x94d049bb133111eb
	k ^= k >> 31
	return k
}

func encodeDay(t time.Time) uint16 {
	if t.IsZero() {
		return 0
	}
	days := int(t.UTC().Sub(dayEpoch) / (24 * time.Hour))
	if days < 1 {
		return 1
	}
	if days > 65535 {
		return 65535
	}
	return uint16(days)
}

func (kh *knownHashes) findSlotLocked(k uint64) (int, bool) {
	mask := uint64(len(kh.keys) - 1)
	idx := int(mix64(k) & mask)
	for {
		cur := kh.keys[idx]
		if cur == 0 {
			return idx, false
		}
		if cur == k {
			return idx, true
		}
		idx = (idx + 1) & int(mask)
	}
}

func (kh *knownHashes) growLocked() {
	oldKeys := kh.keys
	oldDays := kh.days
	newCap := len(oldKeys) * 2
	kh.keys = make([]uint64, newCap)
	kh.days = make([]uint16, newCap)
	mask := uint64(newCap - 1)

	for i, k := range oldKeys {
		if k == 0 {
			continue
		}
		idx := int(mix64(k) & mask)
		for kh.keys[idx] != 0 {
			idx = (idx + 1) & int(mask)
		}
		kh.keys[idx] = k
		kh.days[idx] = oldDays[i]
	}
}

func (kh *knownHashes) insertLocked(k uint64, day uint16) {
	if (kh.count+1)*10 >= len(kh.keys)*7 {
		kh.growLocked()
	}
	idx, found := kh.findSlotLocked(k)
	if found {
		if day > kh.days[idx] {
			kh.days[idx] = day
		}
		return
	}
	kh.keys[idx] = k
	kh.days[idx] = day
	kh.count++
}

func (kh *knownHashes) Lookup(id protocol.ID) (known bool, lastScrapeDay uint16) {
	k := hashKey(id)
	kh.mu.RLock()
	defer kh.mu.RUnlock()

	idx, found := kh.findSlotLocked(k)
	if !found {
		return false, 0
	}
	return true, kh.days[idx]
}

func (kh *knownHashes) MarkKnown(id protocol.ID, day uint16) {
	k := hashKey(id)
	kh.mu.Lock()
	defer kh.mu.Unlock()
	kh.insertLocked(k, day)
}

func (kh *knownHashes) UpdateScrapeDay(id protocol.ID, day uint16) {
	k := hashKey(id)
	kh.mu.Lock()
	defer kh.mu.Unlock()

	if idx, found := kh.findSlotLocked(k); found {
		kh.days[idx] = day
	}
}

func (kh *knownHashes) Len() int {
	kh.mu.RLock()
	defer kh.mu.RUnlock()
	return kh.count
}

func (kh *knownHashes) Load(ctx context.Context, db *sql.DB, saveFilesThreshold uint) error {
	loaded := newKnownHashes()

	rows, err := db.QueryContext(
		ctx,
		"SELECT info_hash, files_status, files_count, files_summary IS NOT NULL FROM torrents",
	)
	if err != nil {
		return err
	}

	for rows.Next() {
		var (
			rawHash     []byte
			filesStatus model.FilesStatus
			filesCount  sql.NullInt64
			hasSummary  bool
		)
		if scanErr := rows.Scan(&rawHash, &filesStatus, &filesCount, &hasSummary); scanErr != nil {
			_ = rows.Close()
			return scanErr
		}
		if len(rawHash) != len(protocol.ID{}) {
			continue
		}

		incomplete := filesStatus == model.FilesStatusNoInfo ||
			(filesStatus != model.FilesStatusSingle && !filesCount.Valid) ||
			(filesStatus == model.FilesStatusOverThreshold && uint(filesCount.Int64) <= saveFilesThreshold) ||
			(filesStatus != model.FilesStatusSingle && !hasSummary)
		if incomplete {
			continue
		}

		var id protocol.ID
		copy(id[:], rawHash)
		loaded.insertLocked(hashKey(id), 0)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close()
		return rowsErr
	}
	_ = rows.Close()

	srcRows, err := db.QueryContext(
		ctx,
		"SELECT info_hash, updated_at, seeders IS NOT NULL AND leechers IS NOT NULL FROM torrents_torrent_sources WHERE source = 'dht'",
	)
	if err != nil {
		return err
	}
	defer srcRows.Close()

	for srcRows.Next() {
		var (
			rawHash   []byte
			updatedAt any
			hasSL     bool
		)
		if scanErr := srcRows.Scan(&rawHash, &updatedAt, &hasSL); scanErr != nil {
			return scanErr
		}
		if !hasSL || len(rawHash) != len(protocol.ID{}) {
			continue
		}
		ts, ok := parseSQLiteTime(updatedAt)
		if !ok {
			continue
		}
		var id protocol.ID
		copy(id[:], rawHash)
		if idx, found := loaded.findSlotLocked(hashKey(id)); found {
			day := encodeDay(ts)
			if day > loaded.days[idx] {
				loaded.days[idx] = day
			}
		}
	}
	if srcErr := srcRows.Err(); srcErr != nil {
		return srcErr
	}

	kh.mu.Lock()
	kh.keys = loaded.keys
	kh.days = loaded.days
	kh.count = loaded.count
	kh.mu.Unlock()

	return nil
}

var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02 15:04:05Z07:00",
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

func parseSQLiteTime(v any) (time.Time, bool) {
	switch val := v.(type) {
	case time.Time:
		return val, !val.IsZero()
	case []byte:
		return parseSQLiteTimeString(string(val))
	case string:
		return parseSQLiteTimeString(val)
	default:
		return time.Time{}, false
	}
}

func parseSQLiteTimeString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range sqliteTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

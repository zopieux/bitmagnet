package model

import (
	"bytes"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/bitmagnet-io/bitmagnet/internal/protocol/metainfo"
)

const SummaryFormatVersion byte = 0x01
const maxTopExtensions = 8
const maxExtensionLength = 12

type ContentSummary struct {
	TotalFiles  uint64
	TotalSize   uint64
	MaxDepth    uint8
	Depths      [5]uint64
	NameLengths [5]uint64
	Sizes       [8]uint64
	TopExts     []ExtensionEntry
	OtherExts   OtherExtensions
}

type ExtensionEntry struct {
	Extension string `json:"ext"`
	Count     uint64 `json:"count"`
	TotalSize uint64 `json:"size"`
}

type OtherExtensions struct {
	Count        uint64 `json:"count"`
	TotalSize    uint64 `json:"size"`
	DistinctExts uint64 `json:"distinct"`
}

type FileEntry struct {
	Path string
	Size uint64
}

func NewSingleFileSummary(name string, size uint64, ext string) ContentSummary {
	var summary ContentSummary
	summary.TotalFiles = 1
	summary.TotalSize = size
	summary.Depths[0] = 1

	baseName := extractBaseName(name)
	nameLen := len([]rune(baseName))
	switch {
	case nameLen <= 15:
		summary.NameLengths[0] = 1
	case nameLen <= 30:
		summary.NameLengths[1] = 1
	case nameLen <= 60:
		summary.NameLengths[2] = 1
	case nameLen <= 120:
		summary.NameLengths[3] = 1
	default:
		summary.NameLengths[4] = 1
	}

	switch {
	case size == 0:
		summary.Sizes[0] = 1
	case size <= 4*1024:
		summary.Sizes[1] = 1
	case size <= 1024*1024:
		summary.Sizes[2] = 1
	case size <= 20*1024*1024:
		summary.Sizes[3] = 1
	case size <= 200*1024*1024:
		summary.Sizes[4] = 1
	case size <= 1024*1024*1024:
		summary.Sizes[5] = 1
	case size <= 5*1024*1024*1024:
		summary.Sizes[6] = 1
	default:
		summary.Sizes[7] = 1
	}

	cleanExt := sanitizeExtension(ext)
	if cleanExt == "" {
		cleanExt = extractCleanExtension(baseName)
	}
	summary.TopExts = []ExtensionEntry{
		{
			Extension: cleanExt,
			Count:     1,
			TotalSize: size,
		},
	}
	return summary
}

func SummarizeMetainfoFiles(info metainfo.Info) ContentSummary {
	entries := make([]FileEntry, len(info.Files))
	for i, f := range info.Files {
		entries[i] = FileEntry{
			Path: f.DisplayPath(&info),
			Size: uint64(f.Length),
		}
	}
	return SummarizeFileEntries(entries)
}

func SummarizeFileEntries(files []FileEntry) ContentSummary {
	var summary ContentSummary
	if len(files) == 0 {
		return summary
	}

	summary.TotalFiles = uint64(len(files))

	type extAgg struct {
		count uint64
		size  uint64
	}
	extMap := make(map[string]*extAgg)

	for _, f := range files {
		summary.TotalSize += f.Size

		depth := countPathDepth(f.Path)
		if depth > summary.MaxDepth {
			summary.MaxDepth = depth
		}
		if depth >= 4 {
			summary.Depths[4]++
		} else {
			summary.Depths[depth]++
		}

		baseName := extractBaseName(f.Path)
		nameLen := len([]rune(baseName))
		switch {
		case nameLen <= 15:
			summary.NameLengths[0]++
		case nameLen <= 30:
			summary.NameLengths[1]++
		case nameLen <= 60:
			summary.NameLengths[2]++
		case nameLen <= 120:
			summary.NameLengths[3]++
		default:
			summary.NameLengths[4]++
		}

		switch {
		case f.Size == 0:
			summary.Sizes[0]++
		case f.Size <= 4*1024:
			summary.Sizes[1]++
		case f.Size <= 1024*1024:
			summary.Sizes[2]++
		case f.Size <= 20*1024*1024:
			summary.Sizes[3]++
		case f.Size <= 200*1024*1024:
			summary.Sizes[4]++
		case f.Size <= 1024*1024*1024:
			summary.Sizes[5]++
		case f.Size <= 5*1024*1024*1024:
			summary.Sizes[6]++
		default:
			summary.Sizes[7]++
		}

		ext := extractCleanExtension(baseName)
		entry := extMap[ext]
		if entry == nil {
			entry = &extAgg{}
			extMap[ext] = entry
		}
		entry.count++
		entry.size += f.Size
	}

	type rankedExt struct {
		ext   string
		count uint64
		size  uint64
	}
	ranked := make([]rankedExt, 0, len(extMap))
	for ext, agg := range extMap {
		ranked = append(ranked, rankedExt{ext: ext, count: agg.count, size: agg.size})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].size != ranked[j].size {
			return ranked[i].size > ranked[j].size
		}
		return ranked[i].count > ranked[j].count
	})

	topN := len(ranked)
	if topN > maxTopExtensions {
		topN = maxTopExtensions
	}

	summary.TopExts = make([]ExtensionEntry, topN)
	for i := 0; i < topN; i++ {
		summary.TopExts[i] = ExtensionEntry{
			Extension: ranked[i].ext,
			Count:     ranked[i].count,
			TotalSize: ranked[i].size,
		}
	}

	for i := topN; i < len(ranked); i++ {
		summary.OtherExts.Count += ranked[i].count
		summary.OtherExts.TotalSize += ranked[i].size
		summary.OtherExts.DistinctExts++
	}

	return summary
}

func countPathDepth(path string) uint8 {
	var depth uint8
	var lastWasSep bool
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c == '/' || c == '\\' {
			if !lastWasSep && i > 0 && i < len(path)-1 {
				if depth < 255 {
					depth++
				}
			}
			lastWasSep = true
		} else {
			lastWasSep = false
		}
	}
	return depth
}

func extractBaseName(path string) string {
	idx := strings.LastIndexAny(path, "/\\")
	if idx >= 0 && idx < len(path)-1 {
		return path[idx+1:]
	}
	return path
}

func sanitizeExtension(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	var b strings.Builder
	for _, r := range ext {
		if b.Len() >= maxExtensionLength {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func extractCleanExtension(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" || len(ext) == 1 {
		return ""
	}
	return sanitizeExtension(ext)
}

func (s ContentSummary) Encode() []byte {
	buf := make([]byte, 0, 96)
	var varintBuf [10]byte

	putUvarint := func(v uint64) {
		n := binary.PutUvarint(varintBuf[:], v)
		buf = append(buf, varintBuf[:n]...)
	}

	buf = append(buf, SummaryFormatVersion)
	putUvarint(s.TotalFiles)
	buf = append(buf, s.MaxDepth)

	for _, count := range s.Depths {
		putUvarint(count)
	}
	for _, count := range s.NameLengths {
		putUvarint(count)
	}
	for _, count := range s.Sizes {
		putUvarint(count)
	}

	buf = append(buf, byte(len(s.TopExts)))
	for _, e := range s.TopExts {
		extBytes := []byte(e.Extension)
		buf = append(buf, byte(len(extBytes)))
		buf = append(buf, extBytes...)
		putUvarint(e.Count)
		putUvarint(e.TotalSize)
	}

	putUvarint(s.OtherExts.Count)
	putUvarint(s.OtherExts.TotalSize)
	putUvarint(s.OtherExts.DistinctExts)

	return buf
}

func DecodeContentSummary(data []byte) (ContentSummary, error) {
	var s ContentSummary
	if len(data) == 0 {
		return s, fmt.Errorf("empty summary buffer")
	}
	if data[0] != SummaryFormatVersion {
		return s, fmt.Errorf("unsupported summary version: 0x%02x", data[0])
	}

	r := bytes.NewReader(data[1:])
	readUvarint := func() (uint64, error) {
		return binary.ReadUvarint(r)
	}

	var err error
	if s.TotalFiles, err = readUvarint(); err != nil {
		return s, err
	}

	maxDepth, err := r.ReadByte()
	if err != nil {
		return s, err
	}
	s.MaxDepth = maxDepth

	for i := 0; i < len(s.Depths); i++ {
		if s.Depths[i], err = readUvarint(); err != nil {
			return s, err
		}
	}
	for i := 0; i < len(s.NameLengths); i++ {
		if s.NameLengths[i], err = readUvarint(); err != nil {
			return s, err
		}
	}
	for i := 0; i < len(s.Sizes); i++ {
		if s.Sizes[i], err = readUvarint(); err != nil {
			return s, err
		}
	}

	numTopExts, err := r.ReadByte()
	if err != nil {
		return s, err
	}
	s.TopExts = make([]ExtensionEntry, numTopExts)
	for i := 0; i < int(numTopExts); i++ {
		extLen, err := r.ReadByte()
		if err != nil {
			return s, err
		}
		extBytes := make([]byte, extLen)
		if _, err := r.Read(extBytes); err != nil {
			return s, err
		}
		s.TopExts[i].Extension = string(extBytes)

		if s.TopExts[i].Count, err = readUvarint(); err != nil {
			return s, err
		}
		if s.TopExts[i].TotalSize, err = readUvarint(); err != nil {
			return s, err
		}
		s.TotalSize += s.TopExts[i].TotalSize
	}

	if s.OtherExts.Count, err = readUvarint(); err != nil {
		return s, err
	}
	if s.OtherExts.TotalSize, err = readUvarint(); err != nil {
		return s, err
	}
	if s.OtherExts.DistinctExts, err = readUvarint(); err != nil {
		return s, err
	}
	s.TotalSize += s.OtherExts.TotalSize

	return s, nil
}

func (s ContentSummary) Value() (driver.Value, error) {
	if s.TotalFiles == 0 {
		return nil, nil
	}
	return s.Encode(), nil
}

func (s *ContentSummary) Scan(value any) error {
	if value == nil {
		*s = ContentSummary{}
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into ContentSummary", value)
	}
	if len(b) == 0 {
		*s = ContentSummary{}
		return nil
	}
	decoded, err := DecodeContentSummary(b)
	if err != nil {
		return err
	}
	*s = decoded
	return nil
}

func (s ContentSummary) MarshalJSON() ([]byte, error) {
	out := struct {
		FilesCount      uint64            `json:"filesCount"`
		TotalSize       uint64            `json:"totalSize"`
		MaxDepth        uint8             `json:"maxDepth"`
		Depths          map[string]uint64 `json:"depths,omitempty"`
		NameLengths     map[string]uint64 `json:"nameLengths,omitempty"`
		Sizes           map[string]uint64 `json:"sizes,omitempty"`
		Extensions      []ExtensionEntry  `json:"extensions,omitempty"`
		OtherExtensions *OtherExtensions  `json:"otherExtensions,omitempty"`
	}{
		FilesCount: s.TotalFiles,
		TotalSize:  s.TotalSize,
		MaxDepth:   s.MaxDepth,
	}

	depthKeys := [5]string{"0", "1", "2", "3", "4+"}
	for i, count := range s.Depths {
		if count > 0 {
			if out.Depths == nil {
				out.Depths = make(map[string]uint64)
			}
			out.Depths[depthKeys[i]] = count
		}
	}

	lenKeys := [5]string{"1_15", "16_30", "31_60", "61_120", "120+"}
	for i, count := range s.NameLengths {
		if count > 0 {
			if out.NameLengths == nil {
				out.NameLengths = make(map[string]uint64)
			}
			out.NameLengths[lenKeys[i]] = count
		}
	}

	sizeKeys := [8]string{"0", "under_4k", "4k_1m", "1m_20m", "20m_200m", "200m_1g", "1g_5g", "5g+"}
	for i, count := range s.Sizes {
		if count > 0 {
			if out.Sizes == nil {
				out.Sizes = make(map[string]uint64)
			}
			out.Sizes[sizeKeys[i]] = count
		}
	}

	if len(s.TopExts) > 0 {
		out.Extensions = s.TopExts
	}

	if s.OtherExts.Count > 0 || s.OtherExts.TotalSize > 0 || s.OtherExts.DistinctExts > 0 {
		out.OtherExtensions = &s.OtherExts
	}

	return json.Marshal(out)
}

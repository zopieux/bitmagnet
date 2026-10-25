package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentSummary_RoundTrip(t *testing.T) {
	files := []FileEntry{
		{Path: "Album/01 - Song.flac", Size: 43_000_000},
		{Path: "Album/02 - Song.flac", Size: 37_000_000},
		{Path: "Album/cover.jpg", Size: 150_000},
		{Path: "Album/disc.cue", Size: 2_000},
	}
	summary := SummarizeFileEntries(files)
	require.Equal(t, uint64(4), summary.TotalFiles)
	require.Equal(t, uint64(80_152_000), summary.TotalSize)
	require.Equal(t, uint8(1), summary.MaxDepth)
	require.Equal(t, uint64(4), summary.Depths[1])
	require.Len(t, summary.TopExts, 3)

	encoded := summary.Encode()
	require.Less(t, len(encoded), 80)

	decoded, err := DecodeContentSummary(encoded)
	require.NoError(t, err)
	require.Equal(t, summary.TotalFiles, decoded.TotalFiles)
	require.Equal(t, summary.TotalSize, decoded.TotalSize)
	require.Equal(t, summary.MaxDepth, decoded.MaxDepth)
	require.Equal(t, summary.Depths, decoded.Depths)
	require.Equal(t, summary.NameLengths, decoded.NameLengths)
	require.Equal(t, summary.Sizes, decoded.Sizes)
	require.Equal(t, summary.TopExts, decoded.TopExts)
	require.Equal(t, summary.OtherExts, decoded.OtherExts)

	val, err := summary.Value()
	require.NoError(t, err)
	var scanned ContentSummary
	require.NoError(t, scanned.Scan(val))
	require.Equal(t, summary.TotalFiles, scanned.TotalFiles)
}

func TestContentSummary_JSONOmitsZeros(t *testing.T) {
	files := []FileEntry{
		{Path: "Movie.2024.1080p.mkv", Size: 3_500_000_000},
	}
	summary := SummarizeFileEntries(files)
	rawJSON, err := json.Marshal(summary)
	require.NoError(t, err)

	jsonStr := string(rawJSON)
	require.Contains(t, jsonStr, `"depths":{"0":1}`)
	require.NotContains(t, jsonStr, `"1":0`)
	require.NotContains(t, jsonStr, `"2":0`)
	require.NotContains(t, jsonStr, `"otherExtensions"`)
}

func TestContentSummary_SingleFileSummary(t *testing.T) {
	s := NewSingleFileSummary("Test.Show.S01E01.1080p.mkv", 1_500_000_000, "mkv")
	require.Equal(t, uint64(1), s.TotalFiles)
	require.Equal(t, uint64(1_500_000_000), s.TotalSize)
	require.Equal(t, uint8(0), s.MaxDepth)
	require.Equal(t, uint64(1), s.Depths[0])
	require.Equal(t, uint64(1), s.Sizes[6]) // 1GB-5GB
	require.Len(t, s.TopExts, 1)
	require.Equal(t, "mkv", s.TopExts[0].Extension)

	rawJSON, err := json.Marshal(s)
	require.NoError(t, err)
	require.Contains(t, jsonStr(rawJSON), `"filesCount":1`)
	require.NotContains(t, jsonStr(rawJSON), `"otherExtensions"`)
}

func TestContentSummary_ExtremeScale(t *testing.T) {
	files := make([]FileEntry, 100_000)
	for i := 0; i < 100_000; i++ {
		ext := fmt.Sprintf("ext%d", i%5000)
		depth := (i % 6) + 1
		pathParts := make([]string, depth+1)
		for d := 0; d < depth; d++ {
			pathParts[d] = fmt.Sprintf("dir%d", d)
		}
		pathParts[depth] = fmt.Sprintf("file_%06d.%s", i, ext)
		files[i] = FileEntry{
			Path: strings.Join(pathParts, "/"),
			Size: uint64((i * 997) % 50_000_000),
		}
	}

	summary := SummarizeFileEntries(files)
	require.Equal(t, uint64(100_000), summary.TotalFiles)
	require.Equal(t, uint64(4992), summary.OtherExts.DistinctExts)

	encoded := summary.Encode()
	require.Less(t, len(encoded), 170)

	decoded, err := DecodeContentSummary(encoded)
	require.NoError(t, err)
	require.Equal(t, summary.TotalFiles, decoded.TotalFiles)
	require.Equal(t, summary.TotalSize, decoded.TotalSize)
}

func jsonStr(b []byte) string {
	return string(b)
}

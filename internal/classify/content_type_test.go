package classify

import (
	"context"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

func file(path, ext string, size uint) model.TorrentFile {
	f := model.TorrentFile{Path: path, Size: size}
	if ext != "" {
		f.Extension = model.NullString{Valid: true, String: ext}
	}

	return f
}

func files(ext string, size uint, n int) []model.TorrentFile {
	fs := make([]model.TorrentFile, 0, n)
	for i := 0; i < n; i++ {
		fs = append(fs, file("dir/track", ext, size))
	}

	return fs
}

func TestDetectContentType(t *testing.T) {
	const mb = 1_000_000

	tests := []struct {
		name    string
		torrent model.Torrent
		want    model.ContentType
		wantSet bool
	}{
		{
			name:    "audiobook by keyword and audio size",
			torrent: model.Torrent{Name: "The Great Novel (Audiobook)", Files: files("mp3", 10*mb, 10)},
			want:    model.ContentTypeAudiobook, wantSet: true,
		},
		{
			name:    "audiobook by m4b without keyword",
			torrent: model.Torrent{Name: "Some Title", Files: files("m4b", 60*mb, 1)},
			want:    model.ContentTypeAudiobook, wantSet: true,
		},
		{
			name:    "audiobook keyword but audio under 50MB is not audiobook or music",
			torrent: model.Torrent{Name: "Short Audiobook", Files: files("mp3", 40*mb, 1)},
		},
		{
			name:    "audiobook beats music when both match",
			torrent: model.Torrent{Name: "An Audiobook", Files: files("flac", 80*mb, 1)},
			want:    model.ContentTypeAudiobook, wantSet: true,
		},
		{
			name:    "comic by majority bytes",
			torrent: model.Torrent{Name: "Series Vol 1", Files: []model.TorrentFile{file("a.cbz", "cbz", 30*mb), file("notes.txt", "", 1*mb)}},
			want:    model.ContentTypeComic, wantSet: true,
		},
		{
			name:    "ebook beats comic when ebook bytes dominate",
			torrent: model.Torrent{Name: "Bundle", Files: []model.TorrentFile{file("a.cbz", "cbz", 10*mb), file("b.epub", "epub", 20*mb)}},
			want:    model.ContentTypeEbook, wantSet: true,
		},
		{
			name:    "music by extension majority",
			torrent: model.Torrent{Name: "Artist - Album", Files: files("flac", 30*mb, 12)},
			want:    model.ContentTypeMusic, wantSet: true,
		},
		{
			name:    "music by keyword with audio majority",
			torrent: model.Torrent{Name: "Best Of Jazz Compilation", Files: files("mp3", 8*mb, 15)},
			want:    model.ContentTypeMusic, wantSet: true,
		},
		{
			name:    "mp3s without keyword are not music",
			torrent: model.Torrent{Name: "Random Tracks", Files: files("mp3", 8*mb, 15)},
		},
		{
			name:    "software by majority bytes",
			torrent: model.Torrent{Name: "Big Suite 2024", Files: []model.TorrentFile{file("setup.exe", "exe", 100*mb), file("disk.iso", "iso", 900*mb), file("read.me", "", 1*mb)}},
			want:    model.ContentTypeSoftware, wantSet: true,
		},
		{
			name:    "xxx by video majority and keyword",
			torrent: model.Torrent{Name: "Some Porn Video", Files: files("mkv", 800*mb, 1)},
			want:    model.ContentTypeXxx, wantSet: true,
		},
		{
			name:    "video majority without keyword is not xxx",
			torrent: model.Torrent{Name: "A Quiet Drama 2021", Files: files("mkv", 800*mb, 1)},
		},
		{
			name:    "plain documents are not an ebook minority",
			torrent: model.Torrent{Name: "Videos with a manual", Files: []model.TorrentFile{file("movie.mkv", "mkv", 700*mb), file("manual.pdf", "pdf", 5*mb)}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectContentType(tc.torrent)
			if got.Valid != tc.wantSet {
				t.Fatalf("DetectContentType valid = %v, want %v (got %q)", got.Valid, tc.wantSet, got.ContentType)
			}
			if tc.wantSet && got.ContentType != tc.want {
				t.Fatalf("DetectContentType = %q, want %q", got.ContentType, tc.want)
			}
		})
	}
}

func TestClassifyDetectsMusicDespiteYear(t *testing.T) {
	torrent := model.Torrent{
		Name:  "Some Band - Discography (2020)",
		Files: files("flac", 30*mb, 10),
	}

	result, err := Classify(context.Background(), torrent, nil)
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}

	if !result.ContentType.Valid || result.ContentType.ContentType != model.ContentTypeMusic {
		t.Fatalf("ContentType = %q (valid %v), want music", result.ContentType.ContentType, result.ContentType.Valid)
	}
}

func TestClassifyStillDetectsMovie(t *testing.T) {
	torrent := model.Torrent{
		Name:  "The Quiet Movie 2019 1080p WEB-DL",
		Files: files("mkv", 2_000*mb, 1),
	}

	result, err := Classify(context.Background(), torrent, nil)
	if err != nil {
		t.Fatalf("Classify error: %v", err)
	}

	if !result.ContentType.Valid || result.ContentType.ContentType != model.ContentTypeMovie {
		t.Fatalf("ContentType = %q (valid %v), want movie", result.ContentType.ContentType, result.ContentType.Valid)
	}
}

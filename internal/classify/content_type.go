package classify

import (
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/keywords"
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

// This file is a straight-line port of the content-type detection step of
// the upstream core classifier workflow (classifier.core.yml), which ran
// on the CEL workflow engine that this fork removed. The extension lists,
// keyword lists, thresholds and rule order are copied verbatim from that
// workflow. A rule of the form
//
//	files.map(f, f.extension in X ? f.size : -f.size).sum() > N
//
// means: the total size of files whose extension is in X, minus the total
// size of all other files, exceeds N (a signed, majority-by-bytes test).

const mb = 1_000_000 // the workflow's `mb` constant

var (
	audioExtensions = extensionSet("mp3", "wav", "flac", "aac", "ogg", "m4a", "m4b", "mid", "dsf")

	audiobookExtensions = extensionSet("m4b")

	comicExtensions = extensionSet("cb7", "cba", "cbr", "cbt", "cbz")

	ebookExtensions = extensionSet(
		"azw", "azw3", "azw4", "azw8", "chm", "doc", "docx", "djvu", "epub",
		"lit", "mobi", "odt", "pdf", "rtf",
	)

	imageExtensions = extensionSet(
		"jpg", "jpeg", "png", "gif", "bmp", "svg", "dds", "psd", "tif", "tiff", "ico",
	)

	musicExtensions = extensionSet("ape", "dsf", "flac")

	softwareExtensions = extensionSet(
		"apk", "app", "bat", "bin", "deb", "dll", "dmg", "exe", "iso", "jar",
		"lua", "msi", "package", "pkg", "rpm", "sh",
	)

	videoExtensions = extensionSet(
		"mp4", "mkv", "avi", "iso", "mov", "wmv", "flv", "m4v", "mpg", "mpeg", "ts", "vob",
	)
)

var (
	audiobookKeywords = keywords.MustNewRegexFromKeywords(
		"audiobooks?", "books?", "(un)?abridged", "narrated", "novels?", "*biograph*",
	)

	musicKeywords = keywords.MustNewRegexFromKeywords(
		"discography", "music", "album", `\V.?\A.?`, "various artists", "compilation",
		"ep", "lp", "single", "vinyl", "classical", "disco", "folk", "hits", "house",
		"indie", "jazz", "metal", "pop", "reggae", "rock", "trance",
	)

	xxxKeywords = keywords.MustNewRegexFromKeywords(
		"anal", "ass", "blowjobs?", "boob*", "cocks?", "cum*", "dicks?", "erotic*",
		"*fuck*", "*gloryhole*", "hardcore", "kink*", "milf*", "nubile*", "onlyfans",
		"orgasm*", "orgy", "*porn*", "pov", "pussy", "seduc*", "sex*", "slut*",
		"tits?", "threesome", "*wank*", "*xxx*",
	)
)

func extensionSet(exts ...string) map[string]bool {
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		set[e] = true
	}

	return set
}

func fileExtension(f model.TorrentFile) string {
	if !f.Extension.Valid {
		return ""
	}

	return f.Extension.String
}

// signedExtensionBytes sums file sizes, adding files whose extension is in
// the set and subtracting all others (the workflow's majority test).
func signedExtensionBytes(files []model.TorrentFile, set map[string]bool) int64 {
	var sum int64

	for _, f := range files {
		if set[fileExtension(f)] {
			sum += int64(f.Size)
		} else {
			sum -= int64(f.Size)
		}
	}

	return sum
}

// videoExtensionBytes is the signed video-bytes sum used by the workflow
// to decide whether video parsing should run for a typed torrent.
func videoExtensionBytes(files []model.TorrentFile) int64 {
	return signedExtensionBytes(files, videoExtensions)
}

// DetectContentType applies the workflow's ordered find_match rules:
// audiobook, comic, ebook, music, software, xxx. The first matching rule
// wins; no match leaves the content type unset.
func DetectContentType(t model.Torrent) model.NullContentType {
	files := t.Files

	// audiobook: audio-majority over 50MB, and a name keyword or an m4b file
	if signedExtensionBytes(files, audioExtensions) > 50*mb &&
		(audiobookKeywords.MatchString(t.BaseName()) || hasExtension(files, audiobookExtensions)) {
		return model.NewNullContentType(model.ContentTypeAudiobook)
	}

	// comic
	if signedExtensionBytes(files, comicExtensions) > 0 {
		return model.NewNullContentType(model.ContentTypeComic)
	}

	// ebook
	if signedExtensionBytes(files, ebookExtensions) > 0 {
		return model.NewNullContentType(model.ContentTypeEbook)
	}

	// music: music-extension majority, or a name keyword with audio majority
	if signedExtensionBytes(files, musicExtensions) > 0 ||
		(musicKeywords.MatchString(t.BaseName()) && signedExtensionBytes(files, audioExtensions) > 0) {
		return model.NewNullContentType(model.ContentTypeMusic)
	}

	// software
	if signedExtensionBytes(files, softwareExtensions) > 0 {
		return model.NewNullContentType(model.ContentTypeSoftware)
	}

	// xxx: video/image majority, and a keyword in the name or file paths
	if signedExtensionBytes(files, unionExtensions(videoExtensions, imageExtensions)) > 0 &&
		xxxKeywords.MatchString(nameAndPaths(t)) {
		return model.NewNullContentType(model.ContentTypeXxx)
	}

	return model.NullContentType{}
}

func hasExtension(files []model.TorrentFile, set map[string]bool) bool {
	for _, f := range files {
		if set[fileExtension(f)] {
			return true
		}
	}

	return false
}

func unionExtensions(sets ...map[string]bool) map[string]bool {
	union := make(map[string]bool)

	for _, set := range sets {
		for e := range set {
			union[e] = true
		}
	}

	return union
}

// nameAndPaths reproduces the workflow's xxx match target: the torrent
// base name followed by every file's base path, joined with spaces.
func nameAndPaths(t model.Torrent) string {
	parts := make([]string, 0, len(t.Files)+1)
	parts = append(parts, t.BaseName())

	for _, f := range t.Files {
		parts = append(parts, f.BasePath())
	}

	return strings.Join(parts, " ")
}

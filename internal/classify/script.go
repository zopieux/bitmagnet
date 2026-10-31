package classify

import (
	"unicode"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

type Script string

const (
	ScriptCyrillic Script = "cyrillic"
	ScriptKorean   Script = "korean"
	ScriptChinese  Script = "chinese"
	ScriptJapanese Script = "japanese"
	ScriptThai     Script = "thai"
)

var scriptOrder = []Script{
	ScriptCyrillic,
	ScriptKorean,
	ScriptChinese,
	ScriptJapanese,
	ScriptThai,
}

func isKana(r rune) bool {
	return unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		(r >= 0x31F0 && r <= 0x31FF) ||
		(r >= 0xFF66 && r <= 0xFF9F)
}

func DetectScripts(text string) map[Script]struct{} {
	scripts := make(map[Script]struct{})

	var hasHan, hasKana, hasHangul bool

	for _, r := range text {
		switch {
		case isKana(r):
			hasKana = true
		case unicode.Is(unicode.Hangul, r):
			hasHangul = true
		case unicode.Is(unicode.Han, r):
			hasHan = true
		case unicode.Is(unicode.Cyrillic, r):
			scripts[ScriptCyrillic] = struct{}{}
		case unicode.Is(unicode.Thai, r):
			scripts[ScriptThai] = struct{}{}
		}
	}

	if hasKana {
		scripts[ScriptJapanese] = struct{}{}
	}

	if hasHangul {
		scripts[ScriptKorean] = struct{}{}
	}

	if hasHan && !hasKana && !hasHangul {
		scripts[ScriptChinese] = struct{}{}
	}

	return scripts
}

func DetectTorrentScripts(torrent model.Torrent) map[Script]struct{} {
	scripts := DetectScripts(torrent.Name)

	for _, file := range torrent.Files {
		for script := range DetectScripts(file.Path) {
			scripts[script] = struct{}{}
		}
	}

	return scripts
}

func MatchScriptAction(
	torrent model.Torrent,
	actions map[Script]ScriptAction,
) (ScriptAction, bool) {
	scripts := DetectTorrentScripts(torrent)
	action := ScriptActionKeep
	matched := false

	for _, script := range scriptOrder {
		if _, ok := scripts[script]; !ok {
			continue
		}

		scriptAction, ok := actions[script]
		if !ok || scriptAction == ScriptActionKeep {
			continue
		}

		matched = true

		if scriptAction == ScriptActionDelete {
			return ScriptActionDelete, true
		}

		action = ScriptActionArchive
	}

	return action, matched
}

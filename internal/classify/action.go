package classify

import (
	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

// Action is the disposition of a torrent that matched a classify rule.
type Action string

const (
	ActionKeep    Action = "keep"
	ActionDelete  Action = "delete"
	ActionArchive Action = "archive"
)

// ScriptActions returns the per-script actions from the config.
func (c Config) ScriptActions() map[Script]ScriptAction {
	return map[Script]ScriptAction{
		ScriptCyrillic: c.CyrillicAction,
		ScriptKorean:   c.KoreanAction,
		ScriptChinese:  c.ChineseAction,
		ScriptJapanese: c.JapaneseAction,
		ScriptThai:     c.ThaiAction,
	}
}

// ActionFor returns the disposition of the torrent per the config.
//
// The first non-keep match wins, in priority order: banned (CSAM), porn,
// scripts. A porn match on a mostly-audio torrent does not count (music
// albums, audiobooks, ...). ActionKeep is returned when nothing matches or
// every matching rule is set to keep.
//
// This is the single source of truth for the classification decision; both
// the processor and the backfill must use it.
func (c Config) ActionFor(torrent model.Torrent) Action {
	if IsBannedTorrent(torrent) && c.BannedAction != BannedActionKeep {
		return Action(c.BannedAction)
	}

	if IsPornTorrent(torrent) && !IsMostlyAudioTorrent(torrent) && c.PornAction != PornActionKeep {
		return Action(c.PornAction)
	}

	if scriptAction, matched := MatchScriptAction(torrent, c.ScriptActions()); matched {
		return Action(scriptAction)
	}

	return ActionKeep
}

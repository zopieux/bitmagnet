package classify

import (
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestDetectScripts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		scripts []Script
	}{
		{
			name:    "cyrillic",
			text:    "Кошка и самурай",
			scripts: []Script{ScriptCyrillic},
		},
		{
			name:    "korean",
			text:    "한국 영화",
			scripts: []Script{ScriptKorean},
		},
		{
			name:    "chinese",
			text:    "中文电影",
			scripts: []Script{ScriptChinese},
		},
		{
			name:    "japanese",
			text:    "日本語のタイトル",
			scripts: []Script{ScriptJapanese},
		},
		{
			name:    "thai",
			text:    "ภาพยนตร์ไทย",
			scripts: []Script{ScriptThai},
		},
		{
			name: "latin",
			text: "The Big Short 2015 1080p",
		},
		{
			name: "domain does not imply script",
			text: "example.ru release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scripts := DetectScripts(tt.text)
			assert.Len(t, scripts, len(tt.scripts))

			for _, script := range tt.scripts {
				assert.Contains(t, scripts, script)
			}
		})
	}
}

func TestJapaneseIsNotChinese(t *testing.T) {
	t.Parallel()

	scripts := DetectScripts("日本語のタイトル")
	assert.Contains(t, scripts, ScriptJapanese)
	assert.NotContains(t, scripts, ScriptChinese)
}

func TestDetectTorrentScripts(t *testing.T) {
	t.Parallel()

	torrent := model.Torrent{
		Name: "The Big Short 2015",
		Files: []model.TorrentFile{
			{Path: "The.Big.Short/Кошка.mkv"},
		},
	}

	assert.Contains(t, DetectTorrentScripts(torrent), ScriptCyrillic)
}

func TestMatchScriptAction(t *testing.T) {
	t.Parallel()

	actions := map[Script]ScriptAction{
		ScriptCyrillic: ScriptActionDelete,
	}

	action, matched := MatchScriptAction(model.Torrent{Name: "Кошка и самурай"}, actions)
	assert.True(t, matched)
	assert.Equal(t, ScriptActionDelete, action)

	action, matched = MatchScriptAction(model.Torrent{Name: "example.ru release"}, actions)
	assert.False(t, matched)
	assert.Equal(t, ScriptActionKeep, action)
}

func TestScriptActionDefaultsToKeep(t *testing.T) {
	t.Parallel()

	config := NewDefaultConfig()
	assert.Equal(t, ScriptActionKeep, config.CyrillicAction)
	assert.Equal(t, ScriptActionKeep, config.KoreanAction)
	assert.Equal(t, ScriptActionKeep, config.ChineseAction)
	assert.Equal(t, ScriptActionKeep, config.JapaneseAction)
	assert.Equal(t, ScriptActionKeep, config.ThaiAction)
}

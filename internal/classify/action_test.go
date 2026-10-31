package classify

import (
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/stretchr/testify/assert"
)

// cfg builds a config with defaults, applying overrides on top.
func cfg(overrides func(*Config)) Config {
	c := NewDefaultConfig()
	overrides(&c)
	return c
}

func TestActionFor(t *testing.T) {
	t.Parallel()

	audioAlbum := model.Torrent{
		Name:        "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44].flac",
		Size:        123456789,
		FilesStatus: model.FilesStatusSingle,
	}
	videoSingle := model.Torrent{
		Name:        "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44].mkv",
		Size:        123456789,
		FilesStatus: model.FilesStatusSingle,
	}

	tests := []struct {
		name    string
		torrent model.Torrent
		config  Config
		want    Action
	}{
		{
			name:    "clean torrent is kept",
			torrent: model.Torrent{Name: "The Big Short 2015"},
			config:  cfg(func(*Config) {}),
			want:    ActionKeep,
		},
		{
			name:    "banned torrent is deleted by default",
			torrent: model.Torrent{Name: "babyheart"},
			config:  cfg(func(*Config) {}),
			want:    ActionDelete,
		},
		{
			name:    "banned takes priority over porn",
			torrent: model.Torrent{Name: "Loli image set"},
			config: cfg(func(c *Config) {
				c.PornAction = PornActionArchive
			}),
			want: ActionDelete,
		},
		{
			name:    "banned kept falls through to no match",
			torrent: model.Torrent{Name: "babyheart"},
			config: cfg(func(c *Config) {
				c.BannedAction = BannedActionKeep
			}),
			want: ActionKeep,
		},
		{
			name:    "banned kept falls through to porn",
			torrent: model.Torrent{Name: "Loli image set"},
			config: cfg(func(c *Config) {
				c.BannedAction = BannedActionKeep
				c.PornAction = PornActionArchive
			}),
			want: ActionArchive,
		},
		{
			name:    "porn torrent is deleted by default",
			torrent: model.Torrent{Name: "cum inside"},
			config:  cfg(func(*Config) {}),
			want:    ActionDelete,
		},
		{
			name:    "porn torrent can be archived",
			torrent: model.Torrent{Name: "cum inside"},
			config: cfg(func(c *Config) {
				c.PornAction = PornActionArchive
			}),
			want: ActionArchive,
		},
		{
			name:    "mostly-audio porn match bails out",
			torrent: audioAlbum,
			config:  cfg(func(*Config) {}),
			want:    ActionKeep,
		},
		{
			name:    "mostly-audio bail-out does not apply to video",
			torrent: videoSingle,
			config:  cfg(func(*Config) {}),
			want:    ActionDelete,
		},
		{
			name:    "porn kept falls through to script",
			torrent: model.Torrent{Name: "cum inside Кошка"},
			config: cfg(func(c *Config) {
				c.PornAction = PornActionKeep
				c.CyrillicAction = ScriptActionArchive
			}),
			want: ActionArchive,
		},
		{
			name:    "script is kept by default",
			torrent: model.Torrent{Name: "Кошка и самурай"},
			config:  cfg(func(*Config) {}),
			want:    ActionKeep,
		},
		{
			name:    "script can be deleted",
			torrent: model.Torrent{Name: "Кошка и самурай"},
			config: cfg(func(c *Config) {
				c.CyrillicAction = ScriptActionDelete
			}),
			want: ActionDelete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.config.ActionFor(tt.torrent))
		})
	}
}

func TestScriptActions(t *testing.T) {
	t.Parallel()

	config := cfg(func(c *Config) {
		c.CyrillicAction = ScriptActionDelete
		c.KoreanAction = ScriptActionArchive
	})

	actions := config.ScriptActions()
	assert.Equal(t, ScriptActionDelete, actions[ScriptCyrillic])
	assert.Equal(t, ScriptActionArchive, actions[ScriptKorean])
	assert.Equal(t, ScriptActionKeep, actions[ScriptChinese])
	assert.Equal(t, ScriptActionKeep, actions[ScriptJapanese])
	assert.Equal(t, ScriptActionKeep, actions[ScriptThai])
}

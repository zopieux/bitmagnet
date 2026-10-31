package classify

import (
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestBannedKeywords(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"pthc collection",
		"PTHC-2024",
		"ptsc videoclip",
		"pedo party",
		"Pedophile material",
		"child porn",
		"child prostitute",
		"preteen sex",
		"underage nude",
		"r@ygold archive",
		"babyheart",
		"Loli image set",
		"This is 儿童色情 video",
		"幼女 video",
		"ロリ image",
		"педофил",
		"детское порно",
		"kinderporno",
		"pornografía infantil",
	} {
		assert.True(t, IsBanned(text), "expected %q to be banned", text)
	}
}

func TestMainstreamNotBanned(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"",
		"The Children of Dune (2003)",
		"Childhood's End - Arthur C. Clarke",
		"Lolita (1962)",
		"Underage Drinking Documentary",
		"pedometer workout",
		"The Child",
	} {
		assert.False(t, IsBanned(text), "expected %q not to be banned", text)
	}
}

func TestLolitaAgeVariants(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"incesto lolita 5y CBaby Pedoman.mpg",
		"ls-magazine Japanese lolita 09yo Blue Orchid lolifuck 12yo+9yo.avi",
		"Home Lolita - 16.rar",
		"Japanese lolita 6 year old whore! .mpg",
		"NoLimitsFun mylola manami 11yo Girlchild Romanian preteen lolita novinha.wmv",
		"10yrs child cjhildporn Russian lolita.mpeg",
		"darkcollection hebe kid bd girls black lolita.mpg",
		"MashaWorld Young lolita compilation Tara 12yo 13yrs.mp4",
		"10yrs nenas de argentina lsm  speedo Dad and Daughter lolipop 1yo (lolita anal).mpg",
		"russkaya_lolita.avi",
		"Pteenlover (lolita anal) screaming baby in diapers.wmv",
		"little girl xxx",
	} {
		assert.True(t, IsBanned(text), "expected %q to be banned", text)
	}
}

func TestIsBannedTorrent(t *testing.T) {
	t.Parallel()

	torrent := model.Torrent{
		Name: "innocent collection",
		Files: []model.TorrentFile{
			{Path: "V/赠品/lolicon/38.jpg"},
		},
	}
	assert.True(t, IsBannedTorrent(torrent))

	torrent.Files = []model.TorrentFile{
		{Path: "全国各地援交妹.mht"},
	}
	assert.False(t, IsBannedTorrent(torrent))
}

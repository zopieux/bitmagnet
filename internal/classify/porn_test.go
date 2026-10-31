package classify

import (
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestPornKeywords(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"Mature4k - Sweet Becky",
		"[Tushy] Kelly Collins",
		"RealJamVR-Yhivi-Strip-Show",
		"[BelAmi] Pillow Talk 3",
		"x-art_lia_black_&_white_1080",
		"Tokyo Hot nkd-064",
		"ABP-449.avi",
		"MIAD-923",
		"FSET299.avi",
		"风骚的家庭教师",
		"东热收藏之靓女",
		"OnlyFans - some set",
		"only-fans compilation",
		"just_for_fans pack",
		"step mom 4k",
		"step-mom 4k",
		"cum inside",
		"gloryhole scenes",
		"hardcore 4k",
		"POV set",
		"lesbians in action",
		"sexual encounters",
		"淫らな夢",
		"セックスシーン",
		"无码中出",

		// Real-world names spot-checked from scratch/bitmagnet.db (must stay
		// flagged, including ones that also contain allowlisted phrases such
		// as "Sex Education" / "Sex Life").
		"1111Customs.22.10.22.Tori.Black.Gives.A.Sex.Education.Lesson.XXX.720p.HEVC.x265.PRT[XvX]",
		"Caribbeancom.26.04.17.Ichika.Nanjo.And.Kurumi.Kokoro.The.Hedonistic.Sex.Life.Of.A.Budget.Share.House.XXX.1080p.MP4-P2P[XC]",
		"HotMilfsFuck.26.02.15.Indra.I.Adore.Sex.Life.Is.A.Porno.XXX.2160p.MP4-P2P[XC]",
		"AmateurCFNM.24.11.14.Kayla.Wild.Helping.His.Sex.Life.XXX.1080p.MP4-WRB[XC]",
		"Tiffany.Leiddi_Sex.life.vol.2.720p.StallionXXX.mp4",
		"How to Have a XXX Sex Life - The Ultimate Vivid Guide",
		"Kiara Lord - Curvy MILF Spices Up Her Sex Life (07.06.2023)_720p.mp4",
		"BrattyMilf - Jennifer White - Stepmom Teaches Sex Education (Oct 8, 2021) rq.mp4",
		"OrgyVR_Gigi Dio_Romi Rain_Taylor Vixxen_Fraternity Hookup_Sex Education_4096p_8K_LR_180.mp4",
		"[NTR] some hentai title",
		"NTR系 催眠",
		"BUG-012-U",
		"Die Sex Nanny Collection",
		"Shemale Sex Pistols 5 DVDRip",

		// New studio and genre keywords plus concatenated (substring) matches,
		// spot-checked from scratch/many_names.txt.
		"MetArt.com 2023-07-09",
		"Marc Dorcel - Jolies Petites Garces",
		"[21Sextury] - ClubSandy - Kassondra Raine (Kassondra's Naughty Massage) [NEW May 1, 2016]",
		"[ExxxtraSmall] Myra Moans Pop My Tiny Cherry (26.06.22)[720p][x264][xFans].mp4",
		"Xnxx v1.53 [18+ Adult Content] Premium Mod Apk {CracksHash}",
		"Futanari",
		"[Umaya (UMA)] Okitsune Futa Hour 3 (SCANMTL)",
		"[jap loli] Nova Scotia Girl 5_CHILD ASIAN-PTHC.wmv",
		"[Ishigaki Takashi] Ecchi na Onee-san-tachi ni Beach de Gyakunansareta Ken (Mahou Shoujo Lyrical Nanoha) [English].zip",
		"[BrazzersExxtra] Madison Ivy - Return Of Ivy (23.07.2017)",
		"ShemaleJapanHardcore - Teen Newhalf Sayaka Ayasaki",
		"[DeviceBondage] Melissa Stratton Testing Melissas Limits (26.08.31)[720p][xFans].mp4",
		"Incesto a Budapest 1995 DVDRip",
		"[OnlyTeenBlowjobs] Blake Blossom [21.11.11][720p].mp4",
		"StraponDreamer - Cleo - The Black Hole",
		"[CzechGangBang] Episodes 13 14 15",

		// The "incesticide" allowlist entry must not suppress a name that
		// also carries another strong indicator.
		"Incesticide XXX",

		// ai-generated packs (user-accepted stretch): must match with no
		// separator, with each common separator, and with repeated spaces.
		"aigenerated",
		"ai_generated",
		"ai generated",
		"ai   generated",
		"[LunarARTNSFW] Elinalise Dragonroad 1 (Patreon) [AI Generated] Torrent",

		// Whole-word NSFW/Nsfw (case-sensitive; lowercase "nsfw" must NOT
		// match). Real names from scratch/many_names.txt.
		"NSFW",
		"Nsfw",
		"[KALA AI NSFW] sailor moon.zip",
		"Nekokoyoshi Cosplay NSFW",
		"Lezhin GL NSFW (Digital) (Cobalt001)",
		"[Niliu Chahui (Sela)] Girls and the King's Tea Party [English] [Lei Scans][NSFW].zip",
		"38GB_EXCLUSIVE_NU Menareslaves Foot Worship Katy Perry Fappening Katheryn Winnick Nsfw Scarlett Johansson Nudew",

		// The "shameless" allowlist entry must not suppress a name that
		// also carries another strong indicator.
		"Shameless NSFW XXX",

		// "hand job" with a space and "edge play" (real names from
		// scratch/many_names.txt spot-checks).
		"MARK'S HEAD BOBBERS & HAND JOBBERS - Ultimate edge play.mp4",
		"Edge play with Little Mina Little .mp4",
		"MHBHJ - Edge play with Remy LaCroix, 2.mp4",
		"Hand Job",
	} {
		assert.True(t, IsPorn(text), "expected %q to be porn", text)
	}
}

func TestMainstreamNotPorn(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"",
		"Sex and the City S01",
		"Sex Education S02",
		"Masters of Sex S01",
		"Hardcore History - The Fall of Rome",
		"The Big Bang Theory S05",
		"Kiss Kiss Bang Bang (2005)",
		"Bang Bang Baby (1974)",
		"Chitty Chitty Bang Bang (1968)",
		"Windows 11 24H2 + RTX-4090",
		"The Big Short (2015) 1080p",
		"histoires-du-cinema-jlg-1988",
		"Something 2160p HEVC-10",
		"Software build Eagle123",
		"Sex Pistols - Never Mind the Bollocks",

		// Real-world names spot-checked from scratch/bitmagnet.db (must stay
		// unflagged).
		"Cumbres borrascosas.DVDRIP.-1992.www.lokotorrents.com",
		"Milford Track",
		"[Anabuki Bouhatei] Shourai Yuubou na Sports Joshi, Kinketsu no Tame Karada o Uru.zip",
		"Britain's Darkest Taboos - Season 3",
		"Canterburské povídky.mkv",
		"Balzac Honoré de - Povídky 5x (2h5m)",
		"Roald Dahl - Povídky (12h30m)",
		"Řecké a římské báje a pověsti",
		"Rozhovory s vrahem Výpověď Charlese Mansona 1. serie (2026)(CZ)[1080p][WEB-DL]",
		"Souboj Titánů (2010) UHDRDV cz en.mkv",
		"Os Jovens Titãs 2003 1ª Temporada Completa [1080p] [WEB-DL] WWW.BLUDV.COM",
		"Katy Perry Ft. Migos - Bon Appétit [Music Video] 1080p [Sbyky].MP4",
		"The.Simpsons.S32E04.Treehouse.of.Horror.XXXI.1080p.HULU.WEB-DL.DD+5.1.H.264-NTb[TGx]",
		"[2014.04.30] キング・クリームソーダ - ゲラゲラポーのうた [CD][FLAC+CUE+LOG+BK][AVCD-55071]",
		"[2022.09.28] シャドーハウス 2nd Season 第1巻 [BD][BDMV+BK][ANZX-16261~2]",
		"ACDC - For Those About To Rock (We Salute You) - 1981,(US),DSF(tracks),(AT-33EV+Verto+UA-7082+NG-6M+TT-81+MR-2000)",
		"Dire Straits - On Every Street - 1991,(2024),(US MFSL),DSF(tracks),(HANA+SAEC-407+GN-10(220)+DDL-120)",
		"K-Holes - Dismania [2012][BFR1028] Hardly Art - HAR-059 2496",
		"up-mod-toca-life-world-build-a-story-v1-78-mod-72724_72724.apk",
		"Sex Education (2019) Season 1 S01 (1080p NF WEBRip x265 HEVC 10bit EAC3 5.1 t3nzin) REPACK",
		"Sex.Life.2021.S01.FRENCH.WEB.XviD-EXTREME",
		"My Sex Life... Or How I Got Into An Argument (1996) [720p] [WEBRip] [YTS.MX]",
		"www.UIndex.org    -    Sex and the City S06E15 Catch-38 1080p HMAX WEB-DL DD5 1 H 264-NTb",
		"Animal.Control.s01.HD1080p.WEBRip.Rus.RuDub.tv",
		"Northern.Exposure.S02.1080p.BluRay.x265-KONTRAST",
		"Phineas_and_Ferb_S5E34_Entrance_Exam.mkv",
		"Open To Anything (Raw Entry Club).avi",
		"Keith Jarrett, Gary Peacock & Paul Motian - The Old Country (Live at the Deer Head Inn) (2024) [FLAC] (24bit-44.1kHz)",
		"A-League.2023.06.03.Grand.Final.Melbourne.City.FC.vs.Central.Coast.Mariners.FC.1080p25.IPTV.EN.mkv",
		"Countryballs - Power Protocol [FitGirl Repack]",
		"Sex Pistols - Never Mind The Bollocks (1977) {TOCP-53098 TOSHIBA-EMI JAPAN}",
		"io-197804",
		"9130378401474861131.la]star-918",
		"Sexo às Avessas (1982)",

		// Anti-regression names from scratch/many_names.txt spot-checks:
		// mainstream titles and near-misses that must stay unflagged.
		"Oxxxymiron  [FLAC]",
		"xxxHOLiC OVA-2 [BDRemux]",
		"The.Simpsons.S33E03.Treehouse.of.Horror.XXXII.720p.WEBRip.2CH.x265.HEVC-PSA.mkv",
		"XXXL_ 13",
		"the.shards.s01.HDR.D.2.0.18+.web-dlrip.xvid.ac3.-hqh",
		"www.UIndex.org    -    Vantage Point 2008 NORDiC 1080p WEB-DL H 264 DDP5 1-ADDICTION",
		"Skyfall 007 James Bond (2012).DvdRip.HQ-BBW",
		"GUNDAM Series (Pack 5) - Complete MSG 00, Unicorn, BBW, AGE - 720p DUAL AUDIO x264",
		"Clark Terry - Porgy & Bess (2004) [EAC-APE]",
		"The.Vixens.Of.Kung.Fu.1975.BDRip.x264-LATENCY[rarbg]",
		"S04E24 - Family Guy - 424 - Peterotica.avi",
		"Atrax Morgue - 1996 - Autoerotic Death",
		"Pawn.Stars.S08E82.Playboys.and.Players.HDTV.x264-mMx",
		"Alma.Wants.New.Playmates.2024.1080p.WEBRip.Legendado.mkv",
		"American.Dad!.S17E03.Cheek.to.Cheek.A.Strippers.Story.1080p.AMZN.WEB-DL.DD+5.1.H.264-CtrlHD",
		"www.UIndex.org    -    Vanderpump Rules S12E10 Threesomes a Crowd 1080p AMZN WEB-DL DDP2 0 H 264-NTb",
		"Prince 1990-06-19 London (Nude Tour 1990 Part 1+2 - Exile Records CD 4001-4002)",
		"Evanovich_Janet-Naughty_Neighbour.epub",
		"Wicked.Part.I.2024.D.WEB-DLRip.1.46Gb.MegaPeer",
		"[Teenfuns]_Margarita(1928-pictures)",
		// "Lolita" must not match the "loli" token and "Futara" must not
		// match the "futa" token (trailing letters break the boundary).
		"Lolita",
		"Futara",

		// Nirvana's "Incesticide" album must not be flagged by the "incest"
		// wildcard (spot-checked from scratch/many_names.txt).
		"1992 - Incesticide",
		"Nirvana - Incesticide - 1990 [FLAC] [RLG]",
		"Nirvana - Incesticide (1992)(US)[24-96][FLAC]",

		// "ai" inside a longer word must not trigger the ai-generated
		// keyword (leading token boundary required).
		"Samurai Generated Stories",

		// Lowercase "nsfw" is not a keyword (case-sensitive NSFW/Nsfw
		// only): software and dataset names must stay unflagged.
		"wallhaven-nsfw",
		"TostAI_nsfw-image-detection-large",
		"turk nsfw pack.stl",

		// Mainstream names carrying an uppercase NSFW tag (spot-checked
		// from scratch/many_names.txt); suppressed via the allowlist.
		"www.UIndex.org    -    Shameless US S06E06 NSFW 1080p REPACK AMZN WEBRip DD5 1 x264-NTb",
		"Tyga-NSFW-(2025)",

		// "edge of play" must not match "edge\\s?play" (only an optional
		// single space between the words), and "handjob" glued to a
		// preceding word has no leading boundary.
		"Edge of Play",
		"Mayashandjobs",
	} {
		assert.False(t, IsPorn(text), "expected %q not to be porn", text)
	}
}

func TestJavCodes(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"ABP-449",
		"MIAD-923",
		"nkd-064",
		"FSET299",
		"SSIS-123",
	} {
		assert.True(t, isJavCode(text), "expected %q to be a JAV code", text)
	}

	for _, text := range []string{
		"RTX-4090",
		"Intel Core i7-9700K",
		"SSD-1080",
		"Windows 11 24H2",
		"histoires-du-cinema-jlg-1988",
		"Something 2160p HEVC-10",
		"Software build Eagle123",

		// Real-world dashed codes from scratch/bitmagnet.db that are not JAV.
		"AVCD-55071",
		"ANZX-16261",
		"UA-7082",
		"SAEC-407",
		"HAR-059",
		"mod-72724",
		"DDL-120",
		"TOCP-53098",
		"io-197804",
		"star-918",
		"STAR-540",
	} {
		assert.False(t, isJavCode(text), "expected %q not to be a JAV code", text)
	}
}

func TestIsPornTorrent(t *testing.T) {
	t.Parallel()

	torrent := model.Torrent{
		Name: "innocent collection",
		Files: []model.TorrentFile{
			{Path: "V/赠品/东热/38.mp4"},
		},
	}
	assert.True(t, IsPornTorrent(torrent))

	torrent.Files = []model.TorrentFile{
		{Path: "01 - Fucking Noob Slug.mp3"},
	}
	assert.False(t, IsPornTorrent(torrent))

	torrent = model.Torrent{
		Name: "some movie",
		Files: []model.TorrentFile{
			{Path: "movie.mkv"},
		},
	}
	assert.False(t, IsPornTorrent(torrent))
}

func TestIsMostlyAudioTorrent(t *testing.T) {
	t.Parallel()

	// Single-file audio (the blink-182 FP case).
	assert.True(t, IsMostlyAudioTorrent(model.Torrent{
		Name:        "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44].flac",
		Size:        123456789,
		FilesStatus: model.FilesStatusSingle,
	}))

	// Single file with no extension in the name: no file info, no bail-out.
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		Name:        "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44]",
		Size:        123456789,
		FilesStatus: model.FilesStatusSingle,
	}))

	// Single-file video.
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		Name:        "some movie.mkv",
		Size:        123456789,
		FilesStatus: model.FilesStatusSingle,
	}))

	// Album tree: flac + cover art + nfo.
	assert.True(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "album/01 - Track One.flac", Size: 30000000},
			{Path: "album/02 - Track Two.flac", Size: 28000000},
			{Path: "album/cover.jpg", Size: 2000000},
			{Path: "album/album.nfo", Size: 1024},
		},
	}))

	// Audiobook with cover art.
	assert.True(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "book/ch01.m4b", Size: 200000000},
			{Path: "book/ch02.m4b", Size: 210000000},
			{Path: "book/cover.png", Size: 500000},
		},
	}))

	// Any video file blocks the bail-out, even a tiny one.
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "album/01 - Track One.flac", Size: 30000000},
			{Path: "album/preview.mp4", Size: 1000000},
		},
	}))

	// Photo set: images without audio.
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "photos/01.jpg", Size: 5000000},
			{Path: "photos/02.jpg", Size: 6000000},
		},
	}))

	// Mostly image: image bytes exceed audio bytes.
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "pack/music.mp3", Size: 10000000},
			{Path: "pack/art/01.jpg", Size: 20000000},
			{Path: "pack/art/02.jpg", Size: 20000000},
		},
	}))

	// Tie between audio and image: bail out.
	assert.True(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "pack/music.mp3", Size: 20000000},
			{Path: "pack/art/01.jpg", Size: 20000000},
		},
	}))

	// No audio at all (documents).
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "docs/book.pdf", Size: 5000000},
		},
	}))

	// No file info at all.
	assert.False(t, IsMostlyAudioTorrent(model.Torrent{
		Name:        "mystery",
		Size:        5000000,
		FilesStatus: model.FilesStatusNoInfo,
	}))
}

func TestPornTorrentAudioBailout(t *testing.T) {
	t.Parallel()

	// The blink-182 FP: the name matches the JAV-code pattern ("blink" +
	// "-182"), but the torrent is a single-file flac release.
	torrent := model.Torrent{
		Name:        "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44].flac",
		Size:        123456789,
		FilesStatus: model.FilesStatusSingle,
	}
	assert.True(t, IsPornTorrent(torrent))
	assert.True(t, IsMostlyAudioTorrent(torrent))
	assert.False(t, IsPornTorrent(torrent) && !IsMostlyAudioTorrent(torrent))

	// The same name with a video file is still porn.
	torrent = model.Torrent{
		Name:        "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44]",
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "blink-182 - California (Deluxe Edition) (2016 Rock) [Flac 24-44].mkv", Size: 123456789},
		},
	}
	assert.True(t, IsPornTorrent(torrent) && !IsMostlyAudioTorrent(torrent))

	// A photo set with a matching name is still porn (images alone never
	// bail out).
	torrent = model.Torrent{
		Name:        "NSFW photo pack",
		FilesStatus: model.FilesStatusMulti,
		Files: []model.TorrentFile{
			{Path: "photos/01.jpg", Size: 5000000},
			{Path: "photos/02.jpg", Size: 6000000},
		},
	}
	assert.True(t, IsPornTorrent(torrent) && !IsMostlyAudioTorrent(torrent))
}

package classify

import (
	"regexp"
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

const (
	bannedWordChar = `[\p{L}\p{N}_]`
	bannedWord     = bannedWordChar + `*`
	bannedNonWord  = `[^\p{L}\p{N}]`
)

var bannedKeywords = []string{
	`babyheart`,
	`boylover`,
	`child\s+(porn` + bannedWord + `|prostitut` + bannedWord + `|sex` + bannedWord + `)`,
	`child\s+sexual\s+abuse`,
	`childlover`,
	`csam`,
	`girllover`,
	`jailbait`,
	`kidd(ie|y)\s+porn` + bannedWord,
	`loli(con)\s+(nude|porn` + bannedWord + `|sex` + bannedWord + `)`,
	`lolicon`,
	`loli\s+(nude|porn` + bannedWord + `|sex` + bannedWord + `)`,
	`loli(s|ta|tas)?\s+(image|images|set|sets|gallery|pack|packs|collection|model|models|dvd|photo|photos)`,
	`shotacon`,
	`minor\s+(nude|porn` + bannedWord + `|sex` + bannedWord + `)`,
	`pedo(phil` + bannedWord + `)?`,
	`preteens?`,
	`russkaya[\s_]*lolita`,
	`ptsc`,
	`pthc`,
	`r@ygold`,
	`reel\s*kiddymov`,
	`underage\s+(nude|porn` + bannedWord + `|sex` + bannedWord + `)`,
	`nymphet` + bannedWord,
	`siberian\s*mouse`,
	`vladmodels?`,
	`shymodels?`,
	`candydoll`,
	`candy\s*girls?`,
	`babyshi(vid)?`,
	`hussyfan`,
	`ls\s*-?\s*(magazine|land|bar|stars|model|models|studio)`,
	`teen\s*(model|models|lolita|preteen|pre-?teen)`,
	`sandra\s*teen`,
	`karpups`,
	`little\s*cuties`,
	`darkcollection`,
	`jailbat` + bannedWord,
	`childp` + bannedWord,
	`childporn` + bannedWord,
	`cpack`,
	`nn\s*girls`,
	`gaylolitaboy`,
	`lolitaboy`,
	`lolifuck` + bannedWord,
	`loliporn` + bannedWord,
	`lolimania`,
	`lolihouse`,
	`lolitabay`,
	`lolita4u`,
	`youngfriends`,
	`youngest`,
	`pornokid`,
	`pornochild`,
	`pornchild`,
	`kinderfick` + bannedWord,
	`kinderficken`,
	`ped[o0](barn|family|kid|mom|mum|mutter|dad|father|uncle|woman|child|lover|porn` + bannedWord + `|sex` + bannedWord + `|phil` + bannedWord + `)?`,
	`p3do` + bannedWord,
	`ped0` + bannedWord,
	`p[ée]do(phil` + bannedWord + `|porn` + bannedWord + `|fil` + bannedWord + `)`,
	`home\s*lolita`,
	`pteenlover`,
	`p[ée]dophil` + bannedWord,
}

var bannedSubstringKeywords = []string{
	`儿童色情`,
	`幼女`,
	`萝莉`,
	`正太`,
	`小学生`,
	`中学生`,
	`児童ポルノ`,
	`ロリ`,
	`ショタ`,
	`援交`,
	`детск` + bannedWord + `\s+порн` + bannedWord,
	`педофил` + bannedWord,
	`несовершеннолетн` + bannedWord,
	`лоли` + bannedWord,
	`kinderporno` + bannedWord,
	`p[äa]dophil` + bannedWord,
	`ped[oó]fil` + bannedWord,
	`p[eé]dophil` + bannedWord,
	`pornograf` + bannedWord + `\s+infantil`,
	`pedopornograf` + bannedWord,
	`pornografia\s+dzieci` + bannedWord,
}

var bannedExtraPatterns = []string{
	`(0?[1-9]|1[0-7])\s*(yo|y/o|y\.o\.?|yrs?|y|years?\s*old|year\s*old|ans?|anos?|años|anni|jahre|jährig` + bannedWord + `)`,
	`loli(ta|tas)?` + bannedNonWord + `*(0?[1-9]|1[0-7])\b`,
	`(0?[1-9]|1[0-7])\s*(-?\s*)?(year|yr|y\.?o\.?)\s*(old|oldest)?` + bannedNonWord + `+loli(ta|tas)?`,
	`loli(ta|tas)?` + bannedNonWord + `*(child|kid|baby|toddler|infant|preteen|pre-?teen|underage|minor|schoolgirl|school\s*girl|incest|rape|diaper|nude|naked|porn` + bannedWord + `|sex` + bannedWord + `|fuck` + bannedWord + `|whore|slut|model|magazine|young` + bannedWord + `|tiny` + bannedWord + `|little` + bannedWord + `|ls\b|hebe|nymph` + bannedWord + `)`,
	`(child|kid|baby|toddler|infant|preteen|pre-?teen|underage|minor|young` + bannedWord + `|little` + bannedWord + `|tiny` + bannedWord + `|hebe|nymph` + bannedWord + `).{0,40}(loli(ta|tas)?|sex` + bannedWord + `|porn` + bannedWord + `|fuck` + bannedWord + `|nude|naked|whore|slut|rape|incest|xxx|ped` + bannedWord + `)`,
	`(sex` + bannedWord + `|porn` + bannedWord + `|fuck` + bannedWord + `|nude|naked|whore|slut|rape|incest|xxx).{0,40}(loli(ta|tas)?|child|kid|baby|toddler|infant|preteen|pre-?teen|underage|minor|young` + bannedWord + `|little` + bannedWord + `|tiny` + bannedWord + `|hebe|nymph` + bannedWord + `)`,
	`(\b(6|7|8|9|10|11|12|13|14|15|16|17)\s*(y|yr|yrs|yo|year|years)\b)`,
}

var (
	bannedKeywordPattern = regexp.MustCompile(
		`(?i)(^|` + bannedNonWord + `)(` + strings.Join(bannedKeywords, "|") + `)(` + bannedNonWord + `|$)`,
	)
	bannedSubstringPattern = regexp.MustCompile(`(?i)(` + strings.Join(bannedSubstringKeywords, "|") + `)`)
	bannedExtraPattern     = regexp.MustCompile(`(?i)(` + strings.Join(bannedExtraPatterns, "|") + `)`)
)

var bannedPathIgnoredExtensions = map[string]struct{}{
	"txt": {}, "nfo": {}, "url": {}, "html": {}, "htm": {}, "mht": {},
	"mhtml": {}, "chm": {}, "ini": {}, "sfv": {}, "md5": {}, "cue": {},
	"log": {}, "apk": {},
}

func IsBanned(text string) bool {
	if text == "" {
		return false
	}

	return bannedKeywordPattern.MatchString(text) ||
		bannedSubstringPattern.MatchString(text) ||
		bannedExtraPattern.MatchString(text)
}

func isBannedPathCandidate(file model.TorrentFile) bool {
	extension := ""
	if file.Extension.Valid {
		extension = file.Extension.String
	} else if inferred := model.FileExtensionFromPath(file.Path); inferred.Valid {
		extension = inferred.String
	}

	extension = strings.ToLower(extension)
	if extension == "" {
		return true
	}

	_, ignored := bannedPathIgnoredExtensions[extension]
	return !ignored
}

func IsBannedTorrent(torrent model.Torrent) bool {
	if IsBanned(torrent.Name) {
		return true
	}

	for _, file := range torrent.Files {
		if isBannedPathCandidate(file) && IsBanned(file.Path) {
			return true
		}
	}

	return false
}

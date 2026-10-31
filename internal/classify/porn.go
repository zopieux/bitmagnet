package classify

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bitmagnet-io/bitmagnet/internal/model"
)

// Mainstream titles which contain adult keywords but are not porn.
var pornMainstreamAllowlist = []string{
	`sex\s+(?:and\s+the\s+city|education|life|lives\s+of\s+college\s+girls|pistols)`,
	`sexo\s+às\s+avessas`,
	`masters\s+of\s+sex`,
	`hardcore\s+history`,
	`(?:the\s+)?big\s+bang\s+theory`,
	`kiss\s+kiss\s+bang\s+bang`,
	`bang\s+bang\s+baby`,
	`chitty\s+chitty\s+bang\s+bang`,
}

var pornKeywords = []string{
	`anal`,
	`ass(?:es)?`,
	`bang(?:bros|ing)`,
	`bdsm`,
	`belami`,
	`blacked(?:raw)?`,
	`brattysis`,
	`busty`,
	`blowjobs?`,
	`bondage`,
	`boob\w*`,
	`brazzers`,
	`caribbeancom`,
	`chaturbate`,
	`cocks?`,
	`creampie`,
	`crossdress\w*`,
	`cuck\w*`,
	`deeper`,
	`cum(?:s)?`,
	`deepthroat`,
	`dicks?`,
	`digitaldesire`,
	`dildo`,
	`erito`,
	`erocomic`,
	`erotic\w*`,
	`evilangel`,
	`fansly\w*`,
	`fc2`,
	`femboy`,
	`familystrokes`,
	`fantia`,
	`fetish`,
	`fakehub`,
	`footjob`,
	`fuck\w*`,
	`gangbang`,
	`gloryhole\w*`,
	`handjob`,
	`hardcore`,
	`hegre`,
	`hentai`,
	`horny`,
	`heyzo`,
	`incest`,
	`interracial`,
	`jav`,
	`javhd`,
	`jizz\w*`,
	`just[\s_-]?for[\s_-]?fans?`,
	`kink(?:y|s)?`,
	`ladyboy`,
	`legalporno`,
	`lesbian[sx]?`,
	`lewd\w*`,
	`masturbat\w*`,
	`manyvids`,
	`mature4k`,
	`mofos`,
	`momcomesfirst`,
	`milf(?:s)?`,
	`naughtyamerica`,
	`netorare`,
	`nubile\w*`,
	`only[\s_-]?fans?`,
	`orgasm\w*`,
	`orgy`,
	`orgyvr`,
	`passionhd`,
	`panties`,
	`panty`,
	`pegging`,
	`playboy`,
	`playmate`,
	`realjamvr`,
	`porn\w*`,
	`pov`,
	`propertys?ex`,
	`prostitut\w*`,
	`puretaboo`,
	`pussy`,
	`realitykings`,
	`seduc\w*`,
	`sexart`,
	`sex\w*`,
	`shemale`,
	`sislovesme`,
	`sissy`,
	`slut\w*`,
	`squirt\w*`,
	`step[\s_-]?(?:daughter|mom|sister|son|dad|father|mother)`,
	`strapon`,
	`stripper`,
	`submissive`,
	`succubus`,
	`swinger`,
	`teamskeet`,
	`threesome`,
	`tits?`,
	`titties`,
	`tonguejob`,
	`topless`,
	`tokyo[\s_-]?hot`,
	`tushy(?:raw)?`,
	`transsexual`,
	`twistys`,
	`vibrator`,
	`vixen`,
	`wank\w*`,
	`x[-\s_]?art`,
	`xxx`,
}

var pornSubstringKeywords = []string{
	`色情`,
	`情色`,
	`成人视频`,
	`成人影片`,
	`成人片`,
	`18禁`,
	`女优`,
	`女優`,
	`无码`,
	`無碼`,
	`无修正`,
	`無修正`,
	`内射`,
	`中出`,
	`中出し`,
	`口交`,
	`肛交`,
	`自慰`,
	`潮喷`,
	`潮吹`,
	`潮吹き`,
	`肉便器`,
	`肉棒`,
	`假阳具`,
	`淫`,
	`エッチ`,
	`えっち`,
	`濡れ`,
	`痴女`,
	`催眠`,
	`調教`,
	`陵辱`,
	`搾乳`,
	`放尿`,
	`寝取`,
	`パイズリ`,
	`手コキ`,
	`足コキ`,
	`アナル`,
	`精液`,
	`ザーメン`,
	`顔射`,
	`売春`,
	`ソープ`,
	`デリヘル`,
	`パパ活`,
	`援交`,
	`盗撮`,
	`隠撮`,
	`着エロ`,
	`ヌード`,
	`风骚`,
	`骚货`,
	`约炮`,
	`约啪`,
	`嫖娼`,
	`乱伦`,
	`迷奸`,
	`强奸`,
	`人妻`,
	`巨乳`,
	`爆乳`,
	`美乳`,
	`熟女`,
	`露出`,
	`偷拍`,
	`里番`,
	`工口`,
	`嫩模`,
	`外围女`,
	`楼凤`,
	`风俗`,
	`風俗`,
	`フェラ`,
	`セックス`,
	`エロ`,
	`官能`,
	`乱交`,
	`痴漢`,
	`おっぱい`,
	`絶頂`,
	`アクメ`,
	`オナニー`,
	`性交`,
	`性愛`,
	`做爱`,
	`性爱`,
	`车震`,
	`开房`,
	`东热`,
	`黑丝`,
	`无套`,
	`大奶`,
}

// pornBoundary matches a single character that is not a Unicode letter or
// digit. Unlike the ASCII-only `\W`, this treats accented and CJK characters
// as word characters, so e.g. "pov" does not match inside "povídky" and
// "tit" does not match inside "Titánů". Underscore is not a letter/digit, so
// it still acts as a boundary (matching the original `[\W_]` behavior).
const pornBoundary = `[^\p{L}\p{N}]`

// pornWeakKeywords are the porn keywords that also occur inside well-known
// mainstream titles ("sex" in "Sex and the City", "hardcore" in "Hardcore
// History"). The mainstream allowlist only suppresses names whose sole porn
// signal is one of these; any other (strong) indicator still flags the name.
var pornWeakKeywords = map[string]struct{}{
	`sex\w*`:   {},
	`hardcore`: {},
}

var (
	pornNonWordSepPattern = regexp.MustCompile(`[^\p{L}\p{N}]+`)

	pornMainstreamPattern = regexp.MustCompile(
		`(?i)(?:^|` + pornBoundary + `)(?:` + strings.Join(pornMainstreamAllowlist, "|") + `)(?:$|` + pornBoundary + `)`,
	)
	pornTokenPattern = regexp.MustCompile(
		`(?i)(?:^|` + pornBoundary + `)(` + strings.Join(pornKeywords, "|") + `)(?:$|` + pornBoundary + `)`,
	)
	// pornStrongTokenPattern is the token pattern restricted to strong
	// (unambiguous) keywords, used to decide whether the mainstream
	// allowlist is allowed to suppress a name.
	pornStrongTokenPattern = regexp.MustCompile(
		`(?i)(?:^|` + pornBoundary + `)(` + strings.Join(strongPornKeywords(), "|") + `)(?:$|` + pornBoundary + `)`,
	)
	// pornWildcardPattern matches explicit porn terms as substrings. "xxx"
	// requires a trailing boundary so Roman numerals such as "XXXI" (31) do
	// not match, while "StallionXXX" (no leading boundary) still does.
	pornWildcardPattern = regexp.MustCompile(`(?i)(?:fuck|gloryhole|porn|wank|xxx(?:$|` + pornBoundary + `))`)
	pornSubstringPattern = regexp.MustCompile(`(?i)(?:` + strings.Join(pornSubstringKeywords, "|") + `)`)
	// pornNtrPattern matches the netorare tag "NTR" as a standalone ASCII
	// word only. A plain substring match would hit English words such as
	// "Control", "KONTRAST" and "Country"; CJK characters are not ASCII
	// letters, so tags like "NTR系" still match.
	pornNtrPattern = regexp.MustCompile(`(?i)(?:^|[^a-z])ntr(?:[^a-z]|$)`)
)

func strongPornKeywords() []string {
	strong := make([]string, 0, len(pornKeywords))
	for _, k := range pornKeywords {
		if _, weak := pornWeakKeywords[k]; weak {
			continue
		}
		strong = append(strong, k)
	}
	return strong
}

var javLabelPrefixes = map[string]struct{}{
	"abp": {}, "ap": {}, "bn": {}, "carib": {}, "cawd": {}, "dvdms": {}, "fc2": {},
	"fsdss": {}, "fset": {}, "gachinco": {}, "gapl": {}, "heyzo": {}, "hodv": {},
	"homa": {}, "ipx": {}, "ipz": {}, "jav": {}, "jul": {}, "jux": {}, "kaad": {},
	"luxu": {}, "mbr": {}, "miaa": {}, "miad": {}, "mide": {}, "midv": {}, "mikr": {},
	"mium": {}, "mmc": {}, "nass": {}, "nkd": {}, "onsg": {}, "pred": {}, "rbd": {},
	"sdnm": {}, "snis": {}, "sone": {}, "snos": {}, "ssis": {}, "ssni": {}, "start": {},
	"sun": {}, "tek": {}, "venu": {}, "waaa": {},
}

var javCodeExcludedPrefixes = map[string]struct{}{
	"aac": {}, "ac": {}, "amd": {}, "anzx": {}, "audi": {}, "avc": {},
	"avcd": {}, "bd": {}, "benz": {}, "bmw": {}, "canon": {}, "cd": {}, "cpu": {},
	"ddp": {}, "ddr": {}, "ddl": {}, "dts": {}, "dvd": {}, "fhd": {}, "ford": {}, "fuji": {},
	"gpu": {}, "gtx": {}, "har": {}, "hdd": {}, "hd": {}, "hdr": {}, "hevc": {},
	"honda": {}, "hyundai": {}, "intel": {}, "io": {}, "ios": {}, "iso": {}, "kia": {},
	"mazda": {}, "mlb": {}, "mod": {}, "mp": {}, "mpeg": {}, "nba": {},
	"nfl": {}, "nhl": {}, "nissan": {}, "nvidia": {}, "nvme": {}, "olympus": {},
	"os": {}, "panasonic": {}, "pci": {}, "psp": {}, "rar": {}, "rtx": {}, "rx": {},
	"saec": {}, "sata": {}, "sdr": {}, "sony": {}, "ssd": {}, "star": {}, "subaru": {},
	"tesla": {}, "tocp": {}, "toyota": {}, "ts": {}, "ua": {}, "ufc": {}, "uhd": {}, "usb": {},
	"vga": {}, "vw": {}, "web": {}, "win": {}, "windows": {}, "wwe": {}, "xbox": {},
	"zip": {},
}

var javCodeExcludedNumbers = map[string]struct{}{
	"480": {}, "540": {}, "576": {}, "720": {}, "1080": {}, "1440": {}, "2160": {}, "4320": {},
}

var (
	javCodePattern = regexp.MustCompile(`(?i)(?:^|` + pornBoundary + `)([a-z]{2,6})-(\d{3,6})(?:$|` + pornBoundary + `)`)
	javCompactPattern = regexp.MustCompile(`(?i)(?:^|` + pornBoundary + `)([a-z]{2,6})(\d{3,5})(?:$|` + pornBoundary + `)`)
)

func isJavCode(text string) bool {
	for _, m := range javCodePattern.FindAllStringSubmatch(text, -1) {
		prefix := strings.ToLower(m[1])
		number := m[2]

		if _, ok := javCodeExcludedPrefixes[prefix]; ok {
			continue
		}
		if _, ok := javCodeExcludedNumbers[number]; ok {
			continue
		}
		if len(number) == 4 {
			if year, err := strconv.Atoi(number); err == nil && year >= 1900 && year <= 2099 {
				continue
			}
		}

		return true
	}

	for _, m := range javCompactPattern.FindAllStringSubmatch(text, -1) {
		if _, ok := javLabelPrefixes[strings.ToLower(m[1])]; ok {
			return true
		}
	}

	return false
}

func isPornMainstreamAllowed(text string) bool {
	if text == "" {
		return false
	}

	normalized := pornNonWordSepPattern.ReplaceAllString(text, " ")
	return pornMainstreamPattern.MatchString(normalized)
}

func IsPorn(text string) bool {
	if text == "" {
		return false
	}

	// A mainstream title (e.g. "Sex and the City") only suppresses the name
	// when its sole porn signal is a weak keyword such as "sex". If any
	// strong indicator ("xxx", a studio name, an explicit term, a JAV code,
	// ...) is present the allowlist must not apply, so titles like
	// "1111Customs ... Sex.Education.Lesson.XXX" are still flagged.
	if isPornMainstreamAllowed(text) && !hasStrongPornIndicator(text) {
		return false
	}

	return pornTokenPattern.MatchString(text) ||
		pornWildcardPattern.MatchString(text) ||
		pornSubstringPattern.MatchString(text) ||
		pornNtrPattern.MatchString(text) ||
		isJavCode(text)
}

// hasStrongPornIndicator reports whether text contains any unambiguous porn
// signal that the mainstream-title allowlist must not suppress.
func hasStrongPornIndicator(text string) bool {
	return pornStrongTokenPattern.MatchString(text) ||
		pornWildcardPattern.MatchString(text) ||
		pornSubstringPattern.MatchString(text) ||
		pornNtrPattern.MatchString(text) ||
		isJavCode(text)
}

var pornPathIgnoredExtensions = map[string]struct{}{
	"txt": {}, "nfo": {}, "cue": {}, "log": {}, "sfv": {}, "md5": {}, "srt": {},
	"sub": {}, "ass": {}, "ssa": {}, "url": {}, "html": {}, "htm": {}, "epub": {},
	"pdf": {}, "mobi": {}, "azw3": {}, "djvu": {}, "cbz": {}, "cbr": {}, "flac": {},
	"alac": {}, "ape": {}, "wav": {}, "dsf": {}, "dff": {}, "mp3": {}, "m4a": {},
	"aac": {}, "ogg": {}, "opus": {}, "wma": {},
}

func isPornPathCandidate(file model.TorrentFile) bool {
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

	_, ignored := pornPathIgnoredExtensions[extension]
	return !ignored
}

func IsPornTorrent(torrent model.Torrent) bool {
	if IsPorn(torrent.Name) {
		return true
	}

	for _, file := range torrent.Files {
		if isPornPathCandidate(file) && IsPorn(file.Path) {
			return true
		}
	}

	return false
}

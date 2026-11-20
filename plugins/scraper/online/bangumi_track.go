package online

import (
	"cmp"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"unicode"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

const (
	bangumiComposerRole = `(?:作[詞词]作曲|作[・·/]?[編编]曲|作曲|[詞词]曲|composers?|composed by)`
	bangumiSingerRole   = `(?:演唱|歌唱|主唱|歌手|ボーカル|歌|唄|vocals?|singers?)`
	bangumiSingleRole   = `(?:` + bangumiComposerRole + `|` + bangumiSingerRole + `|作[詞词]|[編编]曲|lyrics?|arrang(?:ed by|ement|er))`
)

var (
	// 仅折叠控制空白及其相邻空格，保留歌名中的其他半角和全角空格。
	bangumiLayoutSpace = regexp.MustCompile(`\p{Zs}*[\t-\r\x{85}\p{Zl}\p{Zp}][\t-\r\x{85}\p{Z}]*`)
	bangumiSeparator   = regexp.MustCompile(`\p{Zs}+[/／–—-]\p{Zs}+`)
	bangumiFeaturing   = regexp.MustCompile(`(?i)\p{Zs}+(?:(?:feat(?:uring)?|ft)(?:\.\p{Zs}*|\p{Zs}+)|loves\p{Zs}+)\S`)
	bangumiVersion     = regexp.MustCompile(`(?i)\b(?:ver(?:sion)?|mix|remix|remaster(?:ed)?|edit|inst(?:rumental)?|off\s*vocal|karaoke|live|acoustic|demo|reprise|side|part|size)\b|(?:カラオケ|アレンジ|バージョン|伴奏|纯音乐|純音樂|無人声|无人声|版)`)
	bangumiRole        = regexp.MustCompile(`(?i)(?:^|[\s\p{Z}（(【\[|｜/／;；,，、-])(` + bangumiSingleRole + `(?:\p{Zs}*[・·/／&＆、兼]\p{Zs}*` + bangumiSingleRole + `)*)[\s\p{Z}]*[:：][\s\p{Z}]*`)
	bangumiComposer    = regexp.MustCompile(`(?i)` + bangumiComposerRole)
	bangumiSinger      = regexp.MustCompile(`(?i)` + bangumiSingerRole)
)

func bangumiTrackName(raw string) string {
	return strings.TrimSpace(bangumiLayoutSpace.ReplaceAllString(html.UnescapeString(raw), " "))
}

type bangumiTrackHints struct {
	series  map[string]bool
	artists map[string]bool
}

func newBangumiTrackHints(album bangumiRecord, episodes []bangumiEpisode) bangumiTrackHints {
	hints := bangumiTrackHints{series: map[string]bool{}, artists: map[string]bool{}}
	names := map[string]bool{}
	for _, e := range episodes {
		names[bangumiTrackName(e.Name)] = true
		names[bangumiTrackName(e.NameCN)] = true
	}
	// 多版本合辑可能先列原曲，再按歌手分别列出多首翻唱。要求至少两首原曲共享后缀。
	firstSong := map[string]string{}
	for name := range names {
		for _, at := range bangumiSeparator.FindAllStringIndex(name, -1) {
			title, credit := name[:at[0]], name[at[1]:]
			if !names[title] || bangumiVersion.MatchString(credit) || bangumiSeparator.MatchString(credit) {
				continue
			}
			if other := firstSong[credit]; other != "" && other != title {
				hints.series[other], hints.series[title] = true, true
			}
			firstSong[credit] = title
		}
	}
	for _, field := range album.Infobox {
		switch field.Key {
		case "艺术家", "藝術家", "アーティスト", "歌手", "作曲", "演唱":
			var name string
			if json.Unmarshal(field.Value, &name) == nil {
				hints.addArtists(name)
				continue
			}
			var values []struct{ V string }
			if json.Unmarshal(field.Value, &values) == nil {
				for _, value := range values {
					hints.addArtists(value.V)
				}
			}
		}
	}
	return hints
}

func bangumiCreditSeparator(r rune) bool { return strings.ContainsRune("、,&，＆;/／；", r) }

func (h bangumiTrackHints) addArtists(raw string) {
	name := strings.ToLower(bangumiTrackName(raw))
	h.artists[name] = name != ""
	for _, part := range strings.FieldsFunc(name, bangumiCreditSeparator) {
		h.artists[strings.TrimSpace(part)] = true
	}
}

func (h bangumiTrackHints) knownCredit(title, credit string) bool {
	featured := bangumiFeaturing.FindStringIndex(credit)
	if h.artists[strings.ToLower(credit)] || featured != nil && !bangumiSeparator.MatchString(credit[:featured[0]]) {
		return true
	}
	names := strings.FieldsFunc(strings.ToLower(credit), bangumiCreditSeparator)
	if len(names) > 1 {
		known := true
		for _, name := range names {
			known = known && h.artists[strings.TrimSpace(name)]
		}
		if known {
			return true
		}
	}
	return h.series[title] && !bangumiVersion.MatchString(credit) && !bangumiSeparator.MatchString(credit)
}

type bangumiTrackText struct{ title, artist, composer string }

// 角色标签可能附在歌名后，也可能独占译名或说明。没有明确角色时不推断作曲者。
func bangumiLabeledTrack(text string) (bangumiTrackText, bool) {
	matches := bangumiRole.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return bangumiTrackText{}, false
	}
	trimDelimiters := func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("/／｜|;；,，、-", r) }
	title := strings.TrimRightFunc(text[:matches[0][2]], trimDelimiters)
	title = strings.TrimRightFunc(strings.TrimRight(title, "(（[【"), trimDelimiters)
	parsed := bangumiTrackText{title: title}
	var open, close string
	for i, match := range matches {
		until := len(text)
		if i+1 < len(matches) {
			until = matches[i+1][0]
		}
		if match[1] >= until {
			continue
		}
		value := strings.TrimRightFunc(text[match[1]:until], trimDelimiters)
		for _, pair := range [][2]string{{"(", ")"}, {"（", "）"}, {"[", "]"}, {"【", "】"}} {
			if strings.HasSuffix(strings.TrimSpace(text[:match[2]]), pair[0]) {
				open, close = pair[0], pair[1]
			}
		}
		if close != "" && strings.HasSuffix(value, close) && strings.Count(value, close) > strings.Count(value, open) {
			value = strings.TrimSpace(strings.TrimSuffix(value, close))
			open, close = "", ""
		}
		roles := text[match[2]:match[3]]
		if bangumiSinger.MatchString(roles) {
			parsed.artist = appendBangumiCredit(parsed.artist, value)
		}
		if bangumiComposer.MatchString(roles) {
			parsed.composer = appendBangumiCredit(parsed.composer, value)
		}
	}
	return parsed, parsed.artist != "" || parsed.composer != ""
}

func appendBangumiCredit(existing, value string) string {
	if existing == "" || value == "" {
		return cmp.Or(existing, value)
	}
	return existing + " / " + value
}

func (h bangumiTrackHints) name(text string) bangumiTrackText {
	parsed, ok := bangumiLabeledTrack(text)
	if !ok {
		parsed.title = text
	}
	// 优先保留完整的合作署名。无法确认右侧是署名的分隔符仍属于歌名。
	for _, at := range bangumiSeparator.FindAllStringIndex(parsed.title, -1) {
		title, credit := parsed.title[:at[0]], parsed.title[at[1]:]
		if h.knownCredit(title, credit) {
			parsed.title, parsed.artist = title, cmp.Or(parsed.artist, credit)
			break
		}
	}
	return parsed
}

func (h bangumiTrackHints) track(e bangumiEpisode) scraper.Track {
	original := h.name(bangumiTrackName(e.Name))
	chinese := h.name(bangumiTrackName(e.NameCN))
	// 说明中的普通叙述不参与署名，仅读取以角色标签开头的行。
	var desc bangumiTrackText
	for _, line := range strings.Split(e.Desc, "\n") {
		fields, ok := bangumiLabeledTrack(bangumiTrackName(line))
		if ok && fields.title == "" {
			desc.artist = appendBangumiCredit(desc.artist, fields.artist)
			desc.composer = appendBangumiCredit(desc.composer, fields.composer)
		}
	}
	title := cmp.Or(original.title, chinese.title)
	artist := cmp.Or(original.artist, chinese.artist, desc.artist)
	var localizedArtists []scraper.LocalizedTitle
	if chinese.title != "" {
		localizedArtists = addPlatformName(nil, artist, "zh", chinese.artist)
	}
	return scraper.Track{
		ExternalID: numericTrackID(e.ID), Disc: e.Disc, Number: int(e.Sort), Duration: float64(e.Duration),
		Title: title, LocalizedTitles: addPlatformName(nil, title, "zh", chinese.title),
		Artist: artist, LocalizedArtistNames: localizedArtists,
		Composer: cmp.Or(original.composer, chinese.composer, desc.composer),
	}
}

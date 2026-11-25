package music

import (
	"maps"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"golang.org/x/text/unicode/norm"
)

// AlbumTrackEvidence 的曲名和署名来自同一条真实来源曲目，ArtistNames 包含其署名的可信别名。
type AlbumTrackEvidence struct {
	Title       string
	ArtistNames []string
}

// NormalizeMatchName 统一全半角、大小写及连续空白，保留曲名中的版本与标点。
func NormalizeMatchName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(name)), " "))
}

func matchNames(primary string, aliases []scraper.LocalizedTitle) []string {
	names := make([]string, 0, len(aliases)+1)
	if name := NormalizeMatchName(primary); name != "" {
		names = append(names, name)
	}
	for _, alias := range aliases {
		if name := NormalizeMatchName(alias.Title); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// 第一组保留 R&B、AC/DC 等短缩写及首尾符号。其余匹配才是分隔符。
// 带点的 feat./ft. 保留左词界，避免把 Left.Field 等完整艺人名切开。
var creditSeparators = regexp.MustCompile(`(\b[a-z0-9]{1,2}([&+/][a-z0-9]{1,2})+\b|^[&+/]+|[&+/]+$|[&+/]{2,})|\b(feat|ft)\.| (feat|featuring|ft) |[&+/×、;,]`)

// 资料平台与来源标签都可能把多人写在同一项。保留整条署名，只用明确的分隔符识别成员。
func trackCredits(credits []string) map[string]bool {
	names := make(map[string]bool)
	for _, credit := range credits {
		credit = NormalizeMatchName(credit)
		if credit == "" {
			continue
		}
		names[credit] = true
		addMember := func(member string) {
			if name := strings.TrimSpace(member); name != "" {
				names[name] = true
			}
		}
		start := 0
		for _, marker := range creditSeparators.FindAllStringSubmatchIndex(credit, -1) {
			if marker[2] >= 0 {
				continue
			}
			addMember(credit[start:marker[0]])
			start = marker[1]
		}
		if start != 0 {
			addMember(credit[start:])
		}
	}
	return names
}

// MatchesAlbumTracks 要求同一行同时命中曲名和至少一位曲目艺人，不使用专辑署名或作曲者补缺。
func MatchesAlbumTracks(evidence []AlbumTrackEvidence, tracks []scraper.Track) bool {
	byTitle := make(map[string]map[string]bool)
	for _, source := range evidence {
		title := NormalizeMatchName(source.Title)
		artists := trackCredits(source.ArtistNames)
		if title == "" || len(artists) == 0 {
			continue
		}
		if byTitle[title] == nil {
			byTitle[title] = artists
		} else {
			maps.Copy(byTitle[title], artists)
		}
	}
	for _, track := range tracks {
		var credits map[string]bool
		for _, title := range matchNames(track.Title, track.LocalizedTitles) {
			artists := byTitle[title]
			if len(artists) == 0 {
				continue
			}
			if credits == nil {
				credits = trackCredits(matchNames(track.Artist, track.LocalizedArtistNames))
			}
			for name := range credits {
				if artists[name] {
					return true
				}
			}
		}
	}
	return false
}

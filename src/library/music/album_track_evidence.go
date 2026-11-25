package music

import (
	"iter"
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

// 只统一曲末明确的版本标记外框，保留版本词、缩写及其句点。
var dashedTrackVersion = regexp.MustCompile(` -((?:full|short|tv|live) (?:ver\.?|version))-$`)

func trackTitleKey(normalized string) string {
	if !strings.HasSuffix(normalized, "-") {
		return normalized
	}
	return dashedTrackVersion.ReplaceAllString(normalized, " ($1)")
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
var creditSeparators = regexp.MustCompile(`(?i)(\b[a-z0-9]{1,2}([&+/][a-z0-9]{1,2})+\b|^[&+/]+|[&+/]+$|[&+/]{2,})|\b(feat|ft)\.| (feat|featuring|ft) |[&+/×、;,]`)

// ArtistCreditMembers 按署名顺序返回成员，保留艺人名的大小写供平台检索。
func ArtistCreditMembers(credit string) iter.Seq[string] {
	credit = strings.Join(strings.Fields(norm.NFKC.String(credit)), " ")
	return func(yield func(string) bool) {
		start := 0
		for _, marker := range creditSeparators.FindAllStringSubmatchIndex(credit, -1) {
			if marker[2] >= 0 {
				continue
			}
			if name := strings.TrimSpace(credit[start:marker[0]]); name != "" && !yield(name) {
				return
			}
			start = marker[1]
		}
		if name := strings.TrimSpace(credit[start:]); name != "" {
			yield(name)
		}
	}
}

// 资料平台与来源标签都可能把多人写在同一项。核验时也保留未经拆分的整条署名。
func trackCredits(credits []string) map[string]bool {
	names := make(map[string]bool)
	for _, credit := range credits {
		if full := NormalizeMatchName(credit); full != "" {
			names[full] = true
		}
		for member := range ArtistCreditMembers(credit) {
			names[strings.ToLower(member)] = true
		}
	}
	return names
}

// MatchesAlbumTracks 要求同一行同时命中曲名和至少一位曲目艺人，不使用专辑署名或作曲者补缺。
func MatchesAlbumTracks(evidence []AlbumTrackEvidence, tracks []scraper.Track) bool {
	byTitle := make(map[string]map[string]bool)
	for _, source := range evidence {
		title := trackTitleKey(NormalizeMatchName(source.Title))
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
			artists := byTitle[trackTitleKey(title)]
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

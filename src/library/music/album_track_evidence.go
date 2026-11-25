package music

import (
	"iter"
	"maps"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// AlbumTrackEvidence 的曲名和署名来自同一条真实来源曲目，ArtistNames 包含其署名的可信别名。
type AlbumTrackEvidence struct {
	Title       string
	ArtistNames []string
}

// NormalizeMatchName 统一全半角、大小写及连续空白，保留曲名中的版本与标点。
func NormalizeMatchName(name string) string {
	return audiotypes.NormalizeMatchName(name)
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

// ArtistCreditMembers 按署名顺序返回成员，保留艺人名的大小写供平台检索。
func ArtistCreditMembers(credit string) iter.Seq[string] {
	return audiotypes.ArtistCreditMembers(credit)
}

// MatchesAlbumTracks 要求同一行同时命中曲名和至少一位曲目艺人，不使用专辑署名或作曲者补缺。
func MatchesAlbumTracks(evidence []AlbumTrackEvidence, tracks []scraper.Track) bool {
	byTitle := make(map[string]map[string]bool)
	for _, source := range evidence {
		title := trackTitleKey(NormalizeMatchName(source.Title))
		artists := audiotypes.TrackCreditNames(source.ArtistNames)
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
				credits = audiotypes.TrackCreditNames(matchNames(track.Artist, track.LocalizedArtistNames))
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

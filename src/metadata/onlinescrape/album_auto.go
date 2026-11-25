package onlinescrape

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

const maxAutoAlbumCandidates = 3

type AutoAlbumCandidate struct {
	Candidate
	PluginID         string `json:"pluginId"`
	AlbumTracksToken string `json:"albumTracksToken"`
}

// 排名只读取在线查询缓存；仅交给浏览器的候选需要占用预览令牌。
func (s *Service) rankAlbums(ctx context.Context, owner string, album music.AlbumScrapeTarget, artist string) ([]albumMatch, error) {
	var plugins []string
	switch album.SourcePluginID {
	case "musicfs.plugin.source.netease":
		plugins = []string{"musicfs.scraper.netease"}
	case "musicfs.plugin.source.qqmusic":
		plugins = []string{"musicfs.scraper.qqmusic"}
	default:
		settings, err := s.AlbumAutoSettings(ctx, owner)
		if err != nil {
			return nil, err
		}
		plugins = settings.LocalPluginOrder
	}

	// 原始发行编号可直接定位到已启用的平台。同一首歌也可能被别的版本收录。
	if album.Release != nil {
		plugin := "musicfs.scraper." + album.Release.Namespace
		if p := s.plugins[plugin]; slices.Contains(plugins, plugin) && p != nil && p.Descriptor().Available {
			search, cancel := context.WithTimeout(ctx, albumTracksTimeout)
			defer cancel()
			page, err := s.lookup(search, owner, plugin, "albumDetails", scraper.Query{ExternalID: album.Release.ID}, false)
			if err != nil {
				return nil, err
			}
			for _, item := range page.items {
				if item.ID == album.Release.ID {
					return []albumMatch{{plugin: plugin, item: item}}, nil
				}
			}
			return nil, nil
		}
	}

	var ranked []albumMatch
	var firstFailure error
	best := 0
	for i, plugin := range plugins {
		// 每个平台的两种关键词共享原来的时限，后续平台和最终的曲目核验都留有时间。
		budget := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			budget = min(budget, time.Until(deadline)/time.Duration(len(plugins)-i+1))
		}
		search, cancel := context.WithTimeout(ctx, budget)
		matches, err := s.findAlbumEditions(search, owner, plugin, album.Name, artist)
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if firstFailure == nil {
			firstFailure = err
		}
		for _, match := range matches {
			match.priority = i
			ranked = append(ranked, match)
			if match.exact == 1 && (artist == "" || match.artist == 1) {
				best++
			}
		}
		if best >= maxAutoAlbumCandidates {
			break
		}
	}
	if len(ranked) == 0 && firstFailure != nil {
		return nil, firstFailure
	}
	slices.SortStableFunc(ranked, compareAlbumMatch)
	shortlist := ranked[:min(len(ranked), maxAutoAlbumCandidates)]
	// 合辑的专辑署名可能与曲目歌手毫无交集。为仅靠专辑名找到的同名发行版留一席。
	if len(ranked) > len(shortlist) && !slices.ContainsFunc(shortlist, func(m albumMatch) bool { return m.titleOnly }) {
		for _, match := range ranked[len(shortlist):] {
			if match.titleOnly && match.exact == 1 {
				shortlist[len(shortlist)-1] = match
				break
			}
		}
	}
	return shortlist, nil
}

type albumMatch struct {
	item          searchItem
	plugin        string
	exact, artist int
	priority      int
	similarity    float64
	titleOnly     bool
}

// 同一真实来源的首位署名优先；同一曲名的多个音源只计一次。并列时维持原始证据的顺序。
func representativeAlbumArtist(evidence []music.AlbumTrackEvidence) string {
	seen := make(map[[2]string]bool)
	counts := make(map[string]int)
	var first []string
	for _, track := range evidence {
		title := albumLabel(track.Title)
		if title == "" || len(track.ArtistNames) == 0 {
			continue
		}
		var artist string
		for member := range music.ArtistCreditMembers(track.ArtistNames[0]) {
			artist = member
			break
		}
		if !validQuery(scraper.Query{Artist: artist}) {
			continue
		}
		name := albumLabel(artist)
		key := [2]string{name, title}
		if seen[key] {
			continue
		}
		if counts[name] == 0 {
			first = append(first, artist)
		}
		counts[name]++
		seen[key] = true
	}
	var artist string
	for _, name := range first {
		if counts[albumLabel(name)] > counts[albumLabel(artist)] {
			artist = name
		}
	}
	return artist
}

func (s *Service) findAlbumEditions(ctx context.Context, owner, plugin, name, artist string) ([]albumMatch, error) {
	queries := []scraper.Query{{Album: name}}
	if artist != "" {
		queries = append([]scraper.Query{{Album: name, Artist: artist}}, queries...)
	}
	title, credit := albumLabel(name), albumLabel(artist)
	var matches []albumMatch
	seen := make(map[string]int)
	var firstFailure error
	deadline, _ := ctx.Deadline()
	for i, q := range queries {
		search, cancel := context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(queries)-i))
		page, err := s.lookup(search, owner, plugin, "albumDetails", q, false)
		cancel()
		if err != nil {
			if firstFailure == nil {
				firstFailure = err
			}
			continue
		}
		for _, item := range page.items {
			match := rankAlbum(title, credit, item.Item)
			if match.similarity == 0 {
				continue
			}
			match.item, match.plugin, match.titleOnly = item, plugin, q.Artist == ""
			if index, ok := seen[item.ID]; ok {
				if compareAlbumMatch(match, matches[index]) < 0 {
					match.titleOnly = matches[index].titleOnly
					matches[index] = match
				}
				continue
			}
			seen[item.ID] = len(matches)
			matches = append(matches, match)
		}
	}
	return matches, firstFailure
}

func compareAlbumMatch(a, b albumMatch) int {
	return cmp.Or(cmp.Compare(b.exact, a.exact), cmp.Compare(b.similarity, a.similarity),
		cmp.Compare(b.artist, a.artist), cmp.Compare(a.priority, b.priority))
}

func albumLabel(name string) string {
	return music.NormalizeMatchName(name)
}

func rankAlbum(title, artist string, item scraper.Item) albumMatch {
	var match albumMatch
	check := func(name string) {
		name = albumLabel(name)
		if title == name && title != "" {
			match.exact, match.similarity = 1, 1
		} else if match.exact == 0 {
			match.similarity = max(match.similarity, albumNameOverlap(title, name))
		}
	}
	check(item.Title)
	for _, alias := range item.LocalizedTitles {
		check(alias.Title)
	}
	hasArtist := func(credit string) bool {
		for member := range music.ArtistCreditMembers(credit) {
			if albumLabel(member) == artist {
				return true
			}
		}
		return false
	}
	if artist != "" && (hasArtist(item.Artist) || slices.ContainsFunc(item.LocalizedArtistNames, func(alias scraper.LocalizedTitle) bool {
		return hasArtist(alias.Title)
	})) {
		match.artist = 1
	}
	return match
}

// 用相邻字符对的重合比例比较非同名候选，也适用于没有空格的中日韩标题。
func albumNameOverlap(a, b string) float64 {
	left, right := []rune(a), []rune(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	if min(len(left), len(right)) == 1 {
		if strings.Contains(a, b) || strings.Contains(b, a) {
			return 1 / float64(max(len(left), len(right)))
		}
		return 0
	}
	counts := make(map[[2]rune]int, len(left)-1)
	for i := 1; i < len(left); i++ {
		counts[[2]rune{left[i-1], left[i]}]++
	}
	common := 0
	for i := 1; i < len(right); i++ {
		pair := [2]rune{right[i-1], right[i]}
		if counts[pair] > 0 {
			counts[pair]--
			common++
		}
	}
	return float64(2*common) / float64(len(left)+len(right)-2)
}

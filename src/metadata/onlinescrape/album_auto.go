package onlinescrape

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"golang.org/x/text/unicode/norm"
)

const maxAutoAlbumCandidates = 3

type AutoAlbumCandidate struct {
	Candidate
	PluginID string `json:"pluginId"`
}

// SearchAlbum 按音乐源选择平台，用曲库的专辑名搜索，返回最多三个可继续读取的发行版。
func (s *Service) SearchAlbum(ctx context.Context, owner string, album music.AlbumScrapeTarget) ([]AutoAlbumCandidate, error) {
	ranked, err := s.rankAlbums(ctx, owner, album)
	if err != nil {
		return nil, err
	}
	result := make([]AutoAlbumCandidate, len(ranked))
	for i, match := range ranked {
		page := searchPage{items: []searchItem{match.item}, size: match.item.size}
		item := s.issue(owner, match.plugin, "albumDetails", page, mediastore.ContentTarget{})[0]
		result[i] = AutoAlbumCandidate{Candidate: item, PluginID: match.plugin}
	}
	return result, nil
}

// 排名只读取在线查询缓存；仅交给浏览器的候选需要占用预览令牌。
func (s *Service) rankAlbums(ctx context.Context, owner string, album music.AlbumScrapeTarget) ([]albumMatch, error) {
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

	title, artist := albumLabel(album.Name), albumLabel(album.Artist)
	var ranked []albumMatch
	var firstFailure error
	best := 0
	for i, plugin := range plugins {
		// 在控制请求的总时限内为后续平台预留搜索时间。
		budget := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			budget = min(budget, time.Until(deadline)/time.Duration(len(plugins)-i))
		}
		search, cancel := context.WithTimeout(ctx, budget)
		page, err := s.lookup(search, owner, plugin, "albumDetails", scraper.Query{Album: album.Name}, false)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if firstFailure == nil {
				firstFailure = err
			}
			continue
		}
		seen := map[string]bool{}
		for _, item := range page.items {
			match := rankAlbum(title, artist, item.Item)
			if match.similarity == 0 || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			match.item, match.plugin, match.priority = item, plugin, i
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
	slices.SortStableFunc(ranked, func(a, b albumMatch) int {
		return cmp.Or(cmp.Compare(b.exact, a.exact), cmp.Compare(b.similarity, a.similarity),
			cmp.Compare(b.artist, a.artist), cmp.Compare(a.priority, b.priority))
	})
	return ranked[:min(len(ranked), maxAutoAlbumCandidates)], nil
}

type albumMatch struct {
	item          searchItem
	plugin        string
	exact, artist int
	priority      int
	similarity    float64
}

func albumLabel(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(name)), " "))
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
	if artist != "" && (albumLabel(item.Artist) == artist || slices.ContainsFunc(item.LocalizedArtistNames, func(alias scraper.LocalizedTitle) bool {
		return albumLabel(alias.Title) == artist
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

package onlinescrape

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

type LyricsAutoSearchInput struct {
	Query         scraper.Query `json:"query"`
	SourceTrackID string        `json:"sourceTrackId,omitempty"`
	LocalOnly     bool          `json:"localOnly,omitempty"`
}

type LyricsAutoResult struct {
	PluginID string      `json:"pluginId"`
	Items    []Candidate `json:"items"`
}

type LocalLyricsResult struct {
	AssetID, AssetVersion string
	Items                 []scraper.Item
}

type LocalLyricsLookup func(context.Context, string, string, scraper.Query) (LocalLyricsResult, error)

// SetLocalLyricsLookup 在启动时接入按账号和来源曲目授权的本地查询。
func (s *Service) SetLocalLyricsLookup(lookup LocalLyricsLookup) { s.localLyrics = lookup }

// VisitLyricsSources 在第一个有歌词的来源停止。上游服务失败时继续尝试后续来源，保留原始错误。
func (s *Service) VisitLyricsSources(ctx context.Context, owner string, visit func(context.Context, string) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	order, err := s.LyricsAutoSettings(ctx, owner)
	if err != nil {
		return err
	}
	var firstFailure error
	for _, plugin := range order.PluginOrder {
		if err := ctx.Err(); err != nil {
			return err
		}
		found, err := visit(ctx, plugin)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			var failure *domain.Error
			if !errors.As(err, &failure) {
				return err
			}
			switch failure.Code {
			case "SOURCE_UNAVAILABLE", "SOURCE_OFFLINE", "PLUGIN_DISABLED", "UNSUPPORTED", "RATE_LIMITED", "TEMPORARY_FAILURE":
			default:
				return err
			}
			if firstFailure == nil {
				firstFailure = err
			}
			continue
		}
		if found {
			return nil
		}
	}
	return firstFailure
}

// SearchMatchedLyrics 为后台任务读取能直接使用的歌词，不占用浏览器候选的容量。
func (s *Service) SearchMatchedLyrics(ctx context.Context, owner, plugin string, query scraper.Query) ([]Candidate, error) {
	page, err := s.matchedLyrics(ctx, owner, plugin, query)
	if err != nil {
		return nil, err
	}
	items := make([]Candidate, len(page.items))
	for i, entry := range page.items {
		items[i].Item = entry.Item
	}
	return items, nil
}

func (s *Service) matchedLyrics(ctx context.Context, owner, plugin string, q scraper.Query) (searchPage, error) {
	var matched searchPage
	if s.plugins[plugin] == nil || strings.TrimSpace(q.Title) == "" {
		return matched, nil
	}
	// 外部编号和 URL 只在原平台内有效，跨来源自动查询只传曲库里的描述字段。
	page, err := s.lookup(ctx, owner, plugin, "lyrics", scraper.Query{Title: q.Title, Artist: q.Artist, Album: q.Album, Duration: q.Duration}, false)
	if err != nil {
		return matched, err
	}
	for _, entry := range page.items {
		item := entry.Item
		if !matchesLyricsLabel(q.Title, item.Title, item.LocalizedTitles, albumLabel) ||
			(strings.TrimSpace(q.Artist) != "" && !matchesLyricsLabel(q.Artist, item.Artist, item.LocalizedArtistNames, lyricsCredit)) {
			continue
		}
		if _, err := content.Validate("lyrics", []byte(item.Lyrics)); err == nil {
			matched.items = append(matched.items, entry)
			matched.size += entry.size
		}
	}
	return matched, nil
}

func matchesLyricsLabel(want, name string, aliases []scraper.LocalizedTitle, normalize func(string) string) bool {
	want = normalize(want)
	return want == normalize(name) || slices.ContainsFunc(aliases, func(alias scraper.LocalizedTitle) bool {
		return normalize(alias.Title) == want
	})
}

// 曲库用逗号连接多位歌手，平台目录使用斜杠。统一分隔符后仍要求整条署名相同。
func lyricsCredit(value string) string {
	return strings.ReplaceAll(albumLabel(value), " / ", ", ")
}

// SearchLyrics 为手动查询签发短期令牌。本地候选只能选回实际读取的曲目和资产版本。
func (s *Service) SearchLyrics(ctx context.Context, owner string, input LyricsAutoSearchInput, local LocalLyricsLookup) (LyricsAutoResult, error) {
	result := LyricsAutoResult{Items: []Candidate{}}
	if (input.SourceTrackID != "" && !works.ValidID(input.SourceTrackID)) || input.LocalOnly && input.SourceTrackID == "" ||
		!validQuery(input.Query) && !(input.SourceTrackID != "" && input.Query == (scraper.Query{})) {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid lyrics search query or source track")
	}
	if local == nil {
		local = s.localLyrics
	}
	visit := func(ctx context.Context, id string) (bool, error) {
		var page searchPage
		var err error
		var target mediastore.ContentTarget
		if id == LocalLyricsPluginID {
			if local == nil || input.SourceTrackID == "" {
				return false, nil
			}
			lyrics, err := local(ctx, owner, input.SourceTrackID, input.Query)
			if err != nil {
				return false, err
			}
			if len(lyrics.Items) == 0 {
				return false, nil
			}
			if lyrics.AssetID == "" || lyrics.AssetVersion == "" {
				return false, domain.Fail("RESOURCE_CHANGED", "Local lyrics asset version is unavailable")
			}
			target = mediastore.ContentTarget{SourceTrackID: input.SourceTrackID, AssetID: lyrics.AssetID, AssetVersion: lyrics.AssetVersion}
			page, err = preparePage(lyrics.Items, "lyrics", nil)
			if err != nil {
				return false, err
			}
		} else {
			page, err = s.matchedLyrics(ctx, owner, id, input.Query)
			if err != nil {
				return false, err
			}
		}
		if len(page.items) == 0 {
			return false, nil
		}
		result.PluginID, result.Items = id, s.issue(owner, id, "lyrics", page, target)
		return true, nil
	}
	if input.LocalOnly {
		_, err := visit(ctx, LocalLyricsPluginID)
		return result, err
	}
	err := s.VisitLyricsSources(ctx, owner, visit)
	return result, err
}

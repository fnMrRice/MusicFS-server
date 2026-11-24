// Package onlinescrape 管理在线刮削的能力路由和按用户隔离的短期候选。
package onlinescrape

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type Candidate struct {
	scraper.Item
	Token string `json:"candidateToken"`
}

type preview struct {
	owner, token string
	field        works.ScrapedField
	expires      time.Time
	size         int
	target       mediastore.ContentTarget
}

type Service struct {
	plugins     map[string]scraper.Plugin
	gate        chan struct{}
	albums      albumCache
	mu          sync.Mutex
	previews    []preview
	bytes       int
	settings    settings.Repository
	localLyrics LocalLyricsLookup
}

func New(plugins []scraper.Plugin) (*Service, error) {
	s := &Service{plugins: map[string]scraper.Plugin{}, gate: make(chan struct{}, 8), albums: newAlbumCache()}
	for _, p := range plugins {
		d := p.Descriptor()
		if d.Type != "scraper" || !works.ValidID(d.PluginID) || d.Name == "" || len(d.Fields) == 0 || s.plugins[d.PluginID] != nil {
			return nil, errors.New("invalid scraper declaration")
		}
		for i, field := range d.Fields {
			if !scraper.ValidField(field) || slices.Contains(d.Fields[:i], field) {
				return nil, errors.New("invalid scraper capability")
			}
		}
		s.plugins[d.PluginID] = p
	}
	return s, nil
}

func (s *Service) List() []scraper.Descriptor {
	result := make([]scraper.Descriptor, 0, len(s.plugins))
	for _, p := range s.plugins {
		result = append(result, p.Descriptor())
	}
	slices.SortFunc(result, func(a, b scraper.Descriptor) int { return strings.Compare(a.Name, b.Name) })
	return result
}

func validQuery(q scraper.Query) bool {
	for _, v := range []string{q.Title, q.Artist, q.Album, q.ExternalID, q.Country} {
		if utf8.RuneCountInString(v) > 512 || strings.ContainsFunc(v, unicode.IsControl) {
			return false
		}
	}
	if q.SourceURL != "" {
		u, err := url.Parse(q.SourceURL)
		if len(q.SourceURL) > 4096 || strings.ContainsFunc(q.SourceURL, unicode.IsControl) || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return false
		}
	}
	if math.IsNaN(q.Duration) || q.Duration < 0 || q.Duration > 86400 {
		return false
	}
	if len(q.Fingerprint) > 48<<10 || strings.ContainsFunc(q.Fingerprint, unicode.IsSpace) || strings.ContainsFunc(q.Fingerprint, unicode.IsControl) {
		return false
	}
	return strings.TrimSpace(q.Title+q.Artist+q.Album+q.ExternalID+q.SourceURL+q.Fingerprint) != ""
}

func (s *Service) Search(ctx context.Context, owner, id, field string, q scraper.Query) ([]Candidate, error) {
	return s.SearchWithRefresh(ctx, owner, id, field, q, false)
}

func (s *Service) SearchWithRefresh(ctx context.Context, owner, id, field string, q scraper.Query, refresh bool) ([]Candidate, error) {
	page, err := s.lookup(ctx, owner, id, field, q, refresh)
	if err != nil {
		return nil, err
	}
	return s.issue(owner, id, field, *page, mediastore.ContentTarget{}), nil
}

func (s *Service) lookup(ctx context.Context, owner, id, field string, q scraper.Query, refresh bool) (*searchPage, error) {
	p := s.plugins[id]
	if p == nil {
		return nil, domain.Fail("NOT_FOUND", "Unknown scraper plugin")
	}
	d := p.Descriptor()
	if !slices.Contains(d.Fields, field) {
		return nil, domain.Fail("UNSUPPORTED", "Scraper does not provide this field")
	}
	if !d.Available {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Scraper needs configuration")
	}
	if !validQuery(q) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid scraper query")
	}
	album := field == "albumDetails" || field == "albumTracks"
	if refresh && !album {
		return nil, domain.Fail("INVALID_ARGUMENT", "Only album details and tracks support refresh")
	}
	record, err := s.currentSettings(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	load := func(ctx context.Context) (searchPage, error) {
		p, _, release, err := s.configured(id, record, nil)
		if err != nil {
			return searchPage{}, err
		}
		defer release()
		items, err := p.Search(ctx, field, q)
		if err != nil {
			return searchPage{}, err
		}
		return preparePage(items, field, d.ImageHosts)
	}
	var page searchPage
	if album {
		key := albumQuery{albumScope{owner, id, field, record.Revision}, q}
		page, err = s.cachedAlbum(ctx, key, refresh, load)
	} else {
		if err := s.admit(id); err != nil {
			return nil, err
		}
		defer func() { <-s.gate }()
		page, err = load(ctx)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var failure *domain.Error
		if errors.As(err, &failure) {
			if failure.Scraper != nil {
				out, info := *failure, *failure.Scraper
				info.PluginID = id
				out.Scraper = &info
				return nil, &out
			}
			return nil, failure
		}
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Scraper source request failed")
	}
	return &page, nil
}

func (s *Service) admit(id string) error {
	select {
	case s.gate <- struct{}{}:
		return nil
	default:
		return &domain.Error{Code: "RATE_LIMITED", Message: "Scraper search is busy",
			Scraper: &fault.ScraperFailure{PluginID: id, Reason: "busy"}}
	}
}

type searchItem struct {
	scraper.Item
	size int
}

type searchPage struct {
	items []searchItem
	size  int
}

func preparePage(items []scraper.Item, field string, hosts []string) (searchPage, error) {
	var page searchPage
	for _, item := range items {
		if item.ID == "" || item.Title == "" {
			continue
		}
		if item.ImageURL != "" && !scraper.AllowsImageURL(item.ImageURL, hosts) {
			item.ImageURL = ""
		}
		if item.SourceURL != "" {
			u, err := url.Parse(item.SourceURL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				item.SourceURL = ""
			}
		}
		if !hasFieldContent(field, item) {
			continue
		}
		raw, err := json.Marshal(item)
		if err != nil || len(raw) > 1<<20 {
			return searchPage{}, domain.Fail("UNSUPPORTED", "Scraper returned oversized or invalid content")
		}
		if len(page.items) >= 32 || page.size+len(raw) > 2<<20 {
			break
		}
		page.items = append(page.items, searchItem{item, len(raw)})
		page.size += len(raw)
	}
	return page, nil
}

func (s *Service) issue(owner, id, field string, page searchPage, target mediastore.ContentTarget) []Candidate {
	now := time.Now()
	result := make([]Candidate, 0, len(page.items))
	pending := make([]preview, 0, len(page.items))
	for _, entry := range page.items {
		token := domain.NewID()
		result = append(result, Candidate{Item: cloneItem(entry.Item), Token: token})
		pending = append(pending, preview{
			owner: owner, token: token, field: works.ScrapedField{Field: field, PluginID: id, Item: entry.Item},
			expires: now.Add(30 * time.Minute), size: entry.size, target: target,
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.previews) > 0 && (!now.Before(s.previews[0].expires) || len(s.previews)+len(pending) > 1024 || s.bytes+page.size > 16<<20) {
		s.bytes -= s.previews[0].size
		s.previews[0] = preview{}
		s.previews = s.previews[1:]
	}
	s.previews = append(s.previews, pending...)
	s.bytes += page.size
	return result
}

// 搜索缓存和短期候选共享只读数据，对外返回可独立使用的曲目表和别名。
func cloneItem(item scraper.Item) scraper.Item {
	item.LocalizedTitles = slices.Clone(item.LocalizedTitles)
	item.LocalizedArtistNames = slices.Clone(item.LocalizedArtistNames)
	item.Tracks = slices.Clone(item.Tracks)
	for i := range item.Tracks {
		item.Tracks[i].LocalizedTitles = slices.Clone(item.Tracks[i].LocalizedTitles)
		item.Tracks[i].LocalizedArtistNames = slices.Clone(item.Tracks[i].LocalizedArtistNames)
	}
	return item
}

func hasFieldContent(field string, item scraper.Item) bool {
	switch field {
	case "artistImage", "artwork":
		return item.ImageURL != ""
	case "albumTracks":
		return len(item.Tracks) != 0
	case "lyrics":
		return item.Lyrics != ""
	default:
		return true
	}
}

func (s *Service) Candidate(owner, plugin, field, token string) (works.ScrapedField, error) {
	return s.CandidateForTrack(owner, plugin, field, token, mediastore.ContentTarget{})
}

func (s *Service) CandidateForTrack(owner, plugin, field, token string, target mediastore.ContentTarget) (works.ScrapedField, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.previews {
		if p.token == token && p.owner == owner && p.field.Field == field && p.field.PluginID == plugin && time.Now().Before(p.expires) {
			if p.target.SourceTrackID != "" && p.target != target {
				return works.ScrapedField{}, domain.Fail("RESOURCE_CHANGED", "Local lyrics candidate belongs to another source or asset version")
			}
			result := p.field
			result.Item = cloneItem(result.Item)
			return result, nil
		}
	}
	return works.ScrapedField{}, domain.Fail("NOT_FOUND", "Scraper candidate expired or unavailable")
}

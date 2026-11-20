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

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
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
}

type Service struct {
	plugins  map[string]scraper.Plugin
	gate     chan struct{}
	mu       sync.Mutex
	previews []preview
	bytes    int
}

func New(plugins []scraper.Plugin) (*Service, error) {
	s := &Service{plugins: map[string]scraper.Plugin{}, gate: make(chan struct{}, 8)}
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
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		return nil, domain.Fail("RATE_LIMITED", "Scraper search is busy")
	}
	items, err := p.Search(ctx, field, q)
	if err != nil {
		var failure *domain.Error
		if errors.As(err, &failure) {
			return nil, failure
		}
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Scraper source request failed")
	}
	result := []Candidate{}
	pending := []preview{}
	now := time.Now()
	total := 0
	for _, item := range items {
		if item.ID == "" || item.Title == "" {
			continue
		}
		if item.ImageURL != "" && !scraper.AllowsImageURL(item.ImageURL, d.ImageHosts) {
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
			return nil, domain.Fail("UNSUPPORTED", "Scraper returned oversized or invalid content")
		}
		if len(result) >= 32 || total+len(raw) > 2<<20 {
			break
		}
		token := domain.NewID()
		result = append(result, Candidate{Item: item, Token: token})
		pending = append(pending, preview{
			owner: owner, token: token, field: works.ScrapedField{Field: field, PluginID: id, Item: item},
			expires: now.Add(30 * time.Minute), size: len(raw),
		})
		total += len(raw)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.previews) > 0 && (!now.Before(s.previews[0].expires) || len(s.previews)+len(pending) > 1024 || s.bytes+total > 16<<20) {
		s.bytes -= s.previews[0].size
		s.previews[0] = preview{}
		s.previews = s.previews[1:]
	}
	s.previews = append(s.previews, pending...)
	s.bytes += total
	return result, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.previews {
		if p.token == token && p.owner == owner && p.field.Field == field && p.field.PluginID == plugin && time.Now().Before(p.expires) {
			return p.field, nil
		}
	}
	return works.ScrapedField{}, domain.Fail("NOT_FOUND", "Scraper candidate expired or unavailable")
}

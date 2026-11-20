package music

import (
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func invalid() error { return domain.Fail("INVALID_ARGUMENT", "Invalid music library metadata") }

func validText(value string, maximum int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximum && strings.IndexFunc(value, unicode.IsControl) < 0
}

func optional(value **string, maximum int) bool {
	if *value == nil {
		return true
	}
	s := strings.TrimSpace(**value)
	if s == "" {
		*value = nil
		return true
	}
	*value = &s
	return validText(s, maximum)
}

func (a AlbumFields) Normalize() (AlbumFields, error) {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || !validText(a.Name, 256) || !optional(&a.ExternalID, 256) || !optional(&a.Publisher, 256) || !optional(&a.CoverPath, 4096) || !optional(&a.ReleaseDate, 10) || len(a.SourceLinks) > 32 {
		return a, invalid()
	}
	if a.SourceLinks == nil {
		a.SourceLinks = []AlbumSourceLink{}
	}
	seen := map[string]bool{}
	for i := range a.SourceLinks {
		link := &a.SourceLinks[i]
		link.URL = strings.TrimSpace(link.URL)
		u, err := url.Parse(link.URL)
		if err != nil || !validText(link.URL, 4096) || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || !works.ValidID(link.PluginID) || seen[link.PluginID] {
			return a, invalid()
		}
		seen[link.PluginID] = true
	}
	if a.ReleaseDate != nil {
		if _, err := time.Parse(time.DateOnly, *a.ReleaseDate); err != nil {
			return a, invalid()
		}
	}
	return a, nil
}

func (t TrackInput) Normalize() (TrackInput, error) {
	t.Title = strings.TrimSpace(t.Title)
	var err error
	t.LocalizedTitles, err = scraper.NormalizeLocalizedTitles(t.LocalizedTitles)
	if err != nil {
		return t, invalid()
	}
	if t.Title == "" || !validText(t.Title, 256) || !optional(&t.CoverPath, 4096) || !optional(&t.Genre, 128) ||
		t.Year != nil && (*t.Year < 1 || *t.Year > 9999) || t.Rating != nil && (*t.Rating < 1 || *t.Rating > 5) ||
		len(t.ArtistNames) > 128 || len(t.AlbumIDs) > 128 {
		return t, invalid()
	}
	artists := make(map[string]bool)
	for i, name := range t.ArtistNames {
		name = strings.TrimSpace(name)
		if name == "" || !validText(name, 256) || artists[name] {
			return t, invalid()
		}
		t.ArtistNames[i] = name
		artists[name] = true
	}
	albums := make(map[string]bool)
	for _, id := range t.AlbumIDs {
		if !works.ValidID(id) || albums[id] {
			return t, invalid()
		}
		albums[id] = true
	}
	return t, nil
}

func (q Query) Validate() error {
	if q.Limit < 1 || q.Limit > 200 || !validText(q.Q, 256) {
		return invalid()
	}
	if !validText(q.Genre, 128) || q.Unknown != "" && q.Unknown != "artist" && q.Unknown != "album" && q.Unknown != "genre" {
		return invalid()
	}
	if len(q.After) > 1024 || !validText(q.After, 1024) {
		return invalid()
	}
	for _, id := range []string{q.ProviderID, q.AlbumID, q.ArtistID} {
		if id != "" && !works.ValidID(id) {
			return invalid()
		}
	}
	return nil
}

package sqlite

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/catalog"
)

func catalogText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func catalogFormat(t catalogTrack) string {
	if t.Container != nil && *t.Container != "" {
		return strings.ToLower(*t.Container)
	}
	return strings.ToLower(catalogText(t.Codec))
}
func catalogMatches(text, query string) bool {
	text = strings.ToLower(text)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, word) {
			return false
		}
	}
	return true
}
func catalogTrackFilter(owner string, t catalogTrack, q catalog.Query) bool {
	if q.RootID != "" && t.RootID != q.RootID || q.Format != "" && catalogFormat(t) != q.Format || len(q.Formats) > 0 && !slices.Contains(q.Formats, catalogFormat(t)) || q.FavoriteOnly && !t.Favorite || q.AvailableOnly && t.Availability != "available" {
		return false
	}
	if len(q.Availability) > 0 && !slices.Contains(q.Availability, t.Availability) {
		return false
	}
	if q.SourceAlbumID != "" && !slices.Contains(t.albumIDs, q.SourceAlbumID) {
		return false
	}
	if q.DirectoryID != "" && !slices.ContainsFunc(t.directories, func(d catalog.Directory) bool { return d.ID == q.DirectoryID }) {
		return false
	}
	if q.ArtistID != "" && !slices.ContainsFunc(t.Artists, func(name string) bool { return artistID(owner, t.ProviderID, name) == q.ArtistID }) {
		return false
	}
	if len(q.Genres) > 0 && !slices.Contains(q.Genres, strings.ToLower(catalogText(t.Genre))) || len(q.Years) > 0 && (t.Year == nil || !slices.Contains(q.Years, *t.Year)) || len(q.SampleRatesHz) > 0 && (t.SampleRateHz == nil || !slices.Contains(q.SampleRatesHz, *t.SampleRateHz)) {
		return false
	}
	if q.MinBitrateBps != nil && (t.BitrateBps == nil || *t.BitrateBps < *q.MinBitrateBps) || q.MaxBitrateBps != nil && (t.BitrateBps == nil || *t.BitrateBps > *q.MaxBitrateBps) {
		return false
	}
	for _, field := range q.UnknownFields {
		if !catalogUnknown(t, field) {
			return false
		}
	}
	return true
}
func catalogUnknown(t catalogTrack, field string) bool {
	switch field {
	case "artist":
		return len(t.Artists) == 0
	case "album":
		return catalogText(t.Album) == "" && len(t.albumIDs) == 0
	case "genre":
		return catalogText(t.Genre) == ""
	case "year":
		return t.Year == nil
	case "sampleratehz", "sampleRateHz":
		return t.SampleRateHz == nil
	case "bitrate":
		return t.BitrateBps == nil
	case "format":
		return catalogFormat(t) == ""
	}
	return false
}

func catalogArtists(owner string, tracks []catalogTrack) []catalog.Artist {
	items := map[string]*catalog.Artist{}
	albums := map[string]map[string]bool{}
	for _, t := range tracks {
		seen := map[string]bool{}
		for _, name := range t.Artists {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			id := artistID(owner, t.ProviderID, name)
			if items[id] == nil {
				items[id] = &catalog.Artist{ID: id, ProviderInstanceID: t.ProviderID, Name: name, IdentityKind: "localCredit"}
				albums[id] = map[string]bool{}
			}
			items[id].TrackCount++
			for _, album := range t.albumIDs {
				albums[id][album] = true
			}
		}
	}
	result := []catalog.Artist{}
	for id, a := range items {
		a.AlbumCount = len(albums[id])
		result = append(result, *a)
	}
	return result
}

type catalogResult struct {
	id, name string
	data     any
	track    *catalogTrack
}

func catalogResults(owner string, data catalogData, q catalog.Query) ([]json.RawMessage, error) {
	tracks := []catalogTrack{}
	for _, t := range data.tracks {
		if catalogTrackFilter(owner, t, q) {
			tracks = append(tracks, t)
		}
	}
	items := []catalogResult{}
	switch q.Type {
	case "tracks":
		for i := range tracks {
			t := &tracks[i]
			if catalogMatches(strings.Join(append([]string{t.DisplayName, catalogText(t.Title), catalogText(t.Album)}, append(t.Artists, t.albumNames...)...), " "), q.Q) {
				items = append(items, catalogResult{id: t.ID, name: t.DisplayName, data: t.Track, track: t})
			}
		}
	case "artists":
		for _, a := range catalogArtists(owner, tracks) {
			if (q.ArtistID == "" || a.ID == q.ArtistID) && catalogMatches(a.Name, q.Q) {
				items = append(items, catalogResult{id: a.ID, name: a.Name, data: a})
			}
		}
	case "albums":
		selected := map[string]bool{}
		for _, t := range tracks {
			for _, id := range t.albumIDs {
				selected[id] = true
			}
		}
		filterMembers := q.RootID != "" || q.DirectoryID != "" || q.ArtistID != "" || q.Format != "" || len(q.Formats) > 0 || len(q.Genres) > 0 || len(q.Years) > 0 || len(q.SampleRatesHz) > 0 || len(q.Availability) > 0 || len(q.UnknownFields) > 0 || q.FavoriteOnly || q.AvailableOnly || q.MinBitrateBps != nil || q.MaxBitrateBps != nil
		for _, a := range data.albums {
			if (q.SourceAlbumID == "" || a.ID == q.SourceAlbumID) && (!filterMembers || selected[a.ID]) && catalogMatches(a.Name+" "+strings.Join(a.Artists, " "), q.Q) {
				items = append(items, catalogResult{id: a.ID, name: a.Name, data: a})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		comparison := 0
		if q.Type == "tracks" {
			comparison = compareCatalogTracks(*a.track, *b.track, q.Sort)
		} else {
			comparison = strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
		}
		if comparison == 0 {
			return a.id < b.id
		}
		if q.Order == "desc" {
			return comparison > 0
		}
		return comparison < 0
	})
	result := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		raw, err := json.Marshal(item.data)
		if err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, nil
}
func compareCatalogTracks(a, b catalogTrack, field string) int {
	if field == "duration" || field == "bitrate" {
		x, y := a.DurationMs, b.DurationMs
		if field == "bitrate" {
			x, y = a.BitrateBps, b.BitrateBps
		}
		if x == nil && y == nil {
			return 0
		}
		if x == nil {
			return 1
		}
		if y == nil {
			return -1
		}
		if *x < *y {
			return -1
		}
		if *x > *y {
			return 1
		}
		return 0
	}
	x, y := a.DisplayName, b.DisplayName
	switch field {
	case "title":
		x, y = catalogText(a.Title), catalogText(b.Title)
	case "artist":
		x, y = strings.Join(a.Artists, " "), strings.Join(b.Artists, " ")
	case "album":
		x, y = catalogText(a.Album), catalogText(b.Album)
	}
	return strings.Compare(strings.ToLower(x), strings.ToLower(y))
}

package sqlite

import (
	"context"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
)

func (s *Store) CatalogFacets(ctx context.Context, owner, provider string) (catalog.Facets, error) {
	result := catalog.Facets{Directories: []catalog.Directory{}, Formats: []catalog.Format{}, Genres: []catalog.TextFacet{}, Years: []catalog.NumberFacet{}, SampleRatesHz: []catalog.NumberFacet{}, UnknownCounts: map[string]int{}, UnsupportedFields: []string{"playableOnly"}}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	data, err := readCatalogData(ctx, tx, owner, provider)
	if err != nil {
		return result, err
	}
	result.CatalogRevision = data.revision
	directories := map[string]catalog.Directory{}
	formats := map[string]int{}
	genres := map[string]int{}
	years := map[int64]int{}
	rates := map[int64]int{}
	for _, field := range []string{"artist", "album", "genre", "year", "sampleRateHz", "bitrate", "format"} {
		result.UnknownCounts[field] = 0
	}
	for _, t := range data.tracks {
		for _, d := range t.directories {
			previous := directories[d.ID]
			d.TrackCount = previous.TrackCount + 1
			directories[d.ID] = d
		}
		if format := catalogFormat(t); format != "" {
			formats[format]++
		}
		if t.Genre != nil && *t.Genre != "" {
			genres[*t.Genre]++
		}
		if t.Year != nil {
			years[*t.Year]++
		}
		if t.SampleRateHz != nil {
			rates[*t.SampleRateHz]++
		}
		for field := range result.UnknownCounts {
			if catalogUnknown(t, field) {
				result.UnknownCounts[field]++
			}
		}
	}
	for _, item := range directories {
		result.Directories = append(result.Directories, item)
	}
	for name, count := range formats {
		result.Formats = append(result.Formats, catalog.Format{Name: name, TrackCount: count})
	}
	for value, count := range genres {
		result.Genres = append(result.Genres, catalog.TextFacet{Value: value, TrackCount: count})
	}
	for value, count := range years {
		result.Years = append(result.Years, catalog.NumberFacet{Value: value, TrackCount: count})
	}
	for value, count := range rates {
		result.SampleRatesHz = append(result.SampleRatesHz, catalog.NumberFacet{Value: value, TrackCount: count})
	}
	sort.Slice(result.Directories, func(i, j int) bool { return result.Directories[i].ID < result.Directories[j].ID })
	sort.Slice(result.Formats, func(i, j int) bool { return result.Formats[i].Name < result.Formats[j].Name })
	sort.Slice(result.Genres, func(i, j int) bool { return result.Genres[i].Value < result.Genres[j].Value })
	sort.Slice(result.Years, func(i, j int) bool { return result.Years[i].Value < result.Years[j].Value })
	sort.Slice(result.SampleRatesHz, func(i, j int) bool { return result.SampleRatesHz[i].Value < result.SampleRatesHz[j].Value })
	return result, nil
}
func (s *Store) CatalogArtist(ctx context.Context, owner, id string) (catalog.Artist, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return catalog.Artist{}, err
	}
	defer tx.Rollback()
	data, err := readCatalogData(ctx, tx, owner, "")
	if err != nil {
		return catalog.Artist{}, err
	}
	for _, artist := range catalogArtists(owner, data.tracks) {
		if artist.ID == id {
			return artist, nil
		}
	}
	return catalog.Artist{}, domain.Fail("NOT_FOUND", "Source artist not found")
}

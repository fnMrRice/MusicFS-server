package online

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// ITunes 查询地区商店的专辑资料和官方封面，不请求音频。
func ITunes(client *http.Client) scraper.Plugin {
	return newLookupPlugin(client, "itunes", "iTunes Search", []string{"trackDetails", "albumDetails", "albumTracks", "artistDetails"}, searchITunes, "*.mzstatic.com")
}

var itunesPacer = requestPacer{interval: 3 * time.Second}

type itunesEntry struct {
	WrapperType, Kind, CollectionType                             string
	ArtistID, CollectionID, TrackID                               int64
	ArtistName, CollectionName, TrackName                         string
	ArtistLinkURL, ArtistViewURL, CollectionViewURL, TrackViewURL string
	ReleaseDate, PrimaryGenreName                                 string
	ArtworkURL100, ArtworkURL60, ArtworkURL30                     string
	DiscNumber, TrackNumber, TrackCount                           int
	TrackTimeMillis                                               int64
}

func itunesQuery(ctx context.Context, client *http.Client, path string, params url.Values) ([]itunesEntry, error) {
	if err := itunesPacer.wait(ctx); err != nil {
		return nil, err
	}
	var response struct{ Results []itunesEntry }
	err := lookupJSON(ctx, client, "https://itunes.apple.com/"+path, params, nil, &response)
	return response.Results, err
}

func searchITunes(ctx context.Context, client *http.Client, field string, q scraper.Query) ([]scraper.Item, error) {
	country := strings.ToUpper(cmp.Or(q.Country, "US"))
	if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
		return nil, fault.Fail("INVALID_ARGUMENT", "invalid store country")
	}
	entity, term := "song", strings.TrimSpace(q.Title+" "+q.Artist)
	switch field {
	case "albumDetails", "albumTracks":
		entity, term = "album", strings.TrimSpace(cmp.Or(q.Album, q.Title)+" "+q.Artist)
	case "artistDetails":
		entity, term = "musicArtist", cmp.Or(q.Artist, q.Title)
	}
	path := "search"
	params := url.Values{"country": {country}}
	if q.ExternalID != "" {
		id, err := lookupNumericID(q.ExternalID)
		if err != nil {
			return nil, err
		}
		path = "lookup"
		params.Set("id", id)
		if field == "albumTracks" {
			params.Set("entity", "song")
			params.Set("limit", "200")
		}
	} else {
		if term == "" {
			return nil, fault.Fail("INVALID_ARGUMENT", "search term or store ID required")
		}
		params.Set("media", "music")
		params.Set("entity", entity)
		params.Set("term", term)
		params.Set("limit", "8")
		if field == "albumTracks" {
			params.Set("limit", "3")
		}
	}
	entries, err := itunesQuery(ctx, client, path, params)
	if err != nil {
		return nil, err
	}
	results := []scraper.Item{}
	for _, entry := range entries {
		item, ok := entry.item(entity)
		if !ok || (path == "lookup" && item.ID != params.Get("id")) {
			continue
		}
		if field == "albumTracks" {
			rows := entries
			if path == "search" {
				rows, err = itunesQuery(ctx, client, "lookup", url.Values{"country": {country}, "id": {item.ID}, "entity": {"song"}, "limit": {"200"}})
				if err != nil {
					return nil, err
				}
			}
			for _, row := range rows {
				if row.CollectionID == entry.CollectionID && row.Kind == "song" && row.WrapperType == "track" && row.TrackName != "" {
					item.Tracks = append(item.Tracks, scraper.Track{ExternalID: numericTrackID(row.TrackID), Title: row.TrackName, Artist: row.ArtistName, Disc: row.DiscNumber, Number: row.TrackNumber, Duration: float64(row.TrackTimeMillis) / 1000})
				}
			}
			if len(item.Tracks) == 0 {
				continue
			}
			// 即使未达到单次上限，也不能把少于上游声明曲数的响应当作整张专辑。
			if entry.TrackCount > 0 && entry.TrackCount != len(item.Tracks) {
				return nil, errors.New("incomplete store album track list")
			}
			slices.SortStableFunc(item.Tracks, func(a, b scraper.Track) int {
				return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Number, b.Number))
			})
		}
		results = append(results, item)
	}
	enrichStoreNames(ctx, client, country, field, entries, results)
	return results, ctx.Err()
}

func (r itunesEntry) item(entity string) (scraper.Item, bool) {
	var id int64
	item := scraper.Item{Artist: r.ArtistName, Genre: r.PrimaryGenreName}
	for _, raw := range []string{r.ArtworkURL100, r.ArtworkURL60, r.ArtworkURL30} {
		if scraper.AllowsImageURL(raw, []string{"*.mzstatic.com"}) {
			item.ImageURL = raw
			break
		}
	}
	switch entity {
	case "song":
		if r.WrapperType != "track" || r.Kind != "song" {
			return item, false
		}
		id, item.Title, item.SourceURL = r.TrackID, r.TrackName, r.TrackViewURL
		item.Duration, item.Album, item.Date = float64(r.TrackTimeMillis)/1000, r.CollectionName, r.ReleaseDate
	case "album":
		if r.WrapperType != "collection" || !strings.EqualFold(r.CollectionType, "Album") {
			return item, false
		}
		id, item.Title, item.SourceURL = r.CollectionID, r.CollectionName, r.CollectionViewURL
		item.Album, item.Date = r.CollectionName, r.ReleaseDate
	case "musicArtist":
		if r.WrapperType != "artist" {
			return item, false
		}
		id, item.Title, item.SourceURL = r.ArtistID, r.ArtistName, cmp.Or(r.ArtistLinkURL, r.ArtistViewURL)
	}
	item.ID = strconv.FormatInt(id, 10)
	return item, id > 0 && item.Title != ""
}

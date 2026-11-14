package online

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// LRCLIB 提供普通及逐行同步歌词。
func LRCLIB(client *http.Client) scraper.Plugin {
	return newLookupPlugin(client, "lrclib", "LRCLIB", []string{"lyrics"}, searchLRC)
}

var lrcPacer = requestPacer{interval: 700 * time.Millisecond}

type lrcRecord struct {
	ID                                                          int64
	TrackName, ArtistName, AlbumName, SyncedLyrics, PlainLyrics string
	Duration                                                    float64
	Instrumental                                                bool
}

func searchLRC(ctx context.Context, client *http.Client, _ string, q scraper.Query) ([]scraper.Item, error) {
	const base = "https://lrclib.net/api/"
	params := url.Values{}
	var records []lrcRecord
	var single lrcRecord
	if err := lrcPacer.wait(ctx); err != nil {
		return nil, err
	}
	if q.ExternalID != "" {
		id, err := lookupNumericID(q.ExternalID)
		if err != nil {
			return nil, err
		}
		if err := lookupJSON(ctx, client, base+"get/"+id, nil, nil, &single); err != nil {
			return nil, err
		}
		if strconv.FormatInt(single.ID, 10) != id {
			return nil, fault.Fail("SOURCE_UNAVAILABLE", "LRCLIB returned a different record")
		}
		records = []lrcRecord{single}
	} else {
		if q.Title == "" {
			return nil, fault.Fail("INVALID_ARGUMENT", "track title or LRCLIB ID required")
		}
		params.Set("track_name", q.Title)
		if q.Artist != "" {
			params.Set("artist_name", q.Artist)
		}
		if q.Album != "" {
			params.Set("album_name", q.Album)
		}
		if q.Artist != "" {
			if q.Duration >= 1 && q.Duration <= 3600 {
				params.Set("duration", strconv.FormatInt(int64(math.Round(q.Duration)), 10))
			}
			err := lookupJSON(ctx, client, base+"get", params, nil, &single)
			if err == nil {
				records = []lrcRecord{single}
			} else if !errors.Is(err, errCatalogMissing) {
				return nil, err
			} else if err := lrcPacer.wait(ctx); err != nil {
				return nil, err
			}
		}
		if len(records) == 0 {
			params.Del("duration")
			if err := lookupJSON(ctx, client, base+"search", params, nil, &records); err != nil {
				return nil, err
			}
		}
	}
	items := []scraper.Item{}
	for _, row := range records {
		if row.ID <= 0 || row.TrackName == "" || row.Instrumental {
			continue
		}
		lyrics, format := row.SyncedLyrics, "lrc"
		if strings.TrimSpace(lyrics) == "" {
			lyrics, format = row.PlainLyrics, "text"
		}
		if strings.TrimSpace(lyrics) != "" {
			id := strconv.FormatInt(row.ID, 10)
			items = append(items, scraper.Item{ID: id, Title: row.TrackName, Artist: row.ArtistName, Album: row.AlbumName, Duration: row.Duration, Lyrics: lyrics, Format: format, SourceURL: base + "get/" + id})
		}
	}
	return items, nil
}

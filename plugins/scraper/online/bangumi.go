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

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// Bangumi 使用公开 v0 目录，音乐条目的章节保留碟号和曲序。
func Bangumi(client *http.Client) scraper.Plugin {
	return newLookupPlugin(client, "bangumi", "Bangumi", []string{"albumDetails", "workDetails", "albumTracks", "artwork", "artistDetails", "artistImage"}, searchBangumi, "lain.bgm.tv")
}

const bangumiAPI = "https://api.bgm.tv/v0/"

type bangumiRecord struct {
	ID            int64
	Type          int
	Name          string
	NameCN        string `json:"name_cn"`
	Date, Summary string
	ShortSummary  string `json:"short_summary"`
	Images        struct{ Large, Common, Medium string }
}

func searchBangumi(ctx context.Context, client *http.Client, field string, q scraper.Query) ([]scraper.Item, error) {
	isArtist := field == "artistDetails" || field == "artistImage"
	resource, keyword := "subjects", cmp.Or(q.Album, q.Title)
	limit := "8"
	var filter map[string]any
	if isArtist {
		resource, keyword = "persons", cmp.Or(q.Artist, q.Title)
	} else {
		filter = map[string]any{"type": []int{3}}
	}
	if field == "albumTracks" {
		limit = "3"
	}
	var records []bangumiRecord
	if q.ExternalID != "" {
		id, err := lookupNumericID(q.ExternalID)
		if err != nil {
			return nil, err
		}
		var row bangumiRecord
		if err := lookupJSON(ctx, client, bangumiAPI+resource+"/"+id, nil, nil, &row); err != nil {
			return nil, err
		}
		if strconv.FormatInt(row.ID, 10) == id {
			records = append(records, row)
		}
	} else {
		if keyword == "" {
			return nil, fault.Fail("INVALID_ARGUMENT", "album, artist or Bangumi ID required")
		}
		body := map[string]any{"keyword": keyword}
		if filter != nil {
			body["filter"] = filter
		}
		var page struct{ Data []bangumiRecord }
		if err := lookupJSON(ctx, client, bangumiAPI+"search/"+resource, url.Values{"limit": {limit}}, body, &page); err != nil {
			return nil, err
		}
		records = page.Data
	}
	results := []scraper.Item{}
	for _, row := range records {
		if row.ID <= 0 || (row.Name == "" && row.NameCN == "") || (!isArtist && row.Type != 3) {
			continue
		}
		id := strconv.FormatInt(row.ID, 10)
		title := cmp.Or(row.Name, row.NameCN)
		item := scraper.Item{ID: id, Title: title, Date: row.Date, Description: cmp.Or(row.Summary, row.ShortSummary)}
		if isArtist {
			item.Artist, item.SourceURL = title, "https://bgm.tv/person/"+id
		} else {
			item.Album, item.SourceURL = title, "https://bgm.tv/subject/"+id
		}
		switch field {
		case "artistImage", "artwork":
			item.ImageURL = lookupImage(cmp.Or(row.Images.Large, row.Images.Common, row.Images.Medium), "lain.bgm.tv")
			if item.ImageURL == "" {
				continue
			}
		case "albumTracks":
			tracks, err := bangumiTracks(ctx, client, id)
			if err != nil {
				return nil, err
			}
			if len(tracks) == 0 {
				continue
			}
			item.Tracks = tracks
		}
		results = append(results, item)
	}
	return results, nil
}

func bangumiTracks(ctx context.Context, client *http.Client, id string) ([]scraper.Track, error) {
	const pageSize, maxTracks = 100, 2000
	tracks := []scraper.Track{}
	expectedTotal := -1
	for offset := 0; ; {
		var page struct {
			Total int
			Data  []struct {
				Name     string
				NameCN   string `json:"name_cn"`
				Disc     int
				Sort     float64
				Duration int `json:"duration_seconds"`
			}
		}
		params := url.Values{"subject_id": {id}, "limit": {strconv.Itoa(pageSize)}, "offset": {strconv.Itoa(offset)}}
		if err := lookupJSON(ctx, client, bangumiAPI+"episodes", params, nil, &page); err != nil {
			return nil, err
		}
		if page.Total > maxTracks || offset+len(page.Data) > maxTracks {
			return nil, errors.New("album track list is too large")
		}
		if page.Total < 0 || expectedTotal >= 0 && page.Total != expectedTotal || offset+len(page.Data) > page.Total {
			return nil, errors.New("album track list changed during lookup")
		}
		expectedTotal = page.Total
		for _, row := range page.Data {
			title := strings.TrimSpace(cmp.Or(row.Name, row.NameCN))
			if title == "" {
				return nil, errors.New("album track title is missing")
			}
			tracks = append(tracks, scraper.Track{Title: title, Disc: row.Disc, Number: int(row.Sort), Duration: float64(row.Duration)})
		}
		offset += len(page.Data)
		if offset >= page.Total {
			break
		}
		if len(page.Data) == 0 {
			return nil, errors.New("incomplete album track list")
		}
	}
	slices.SortStableFunc(tracks, func(a, b scraper.Track) int {
		return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Number, b.Number))
	})
	return tracks, nil
}

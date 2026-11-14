package online

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type acoustIDPlugin struct {
	client *http.Client
	key    string
}

func AcoustID(client *http.Client, key string) scraper.Plugin {
	return acoustIDPlugin{client, strings.TrimSpace(key)}
}

func (p acoustIDPlugin) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: "musicfs.scraper.acoustid", Name: "AcoustID", Type: "scraper",
		Fields: []string{"recordingIdentity"}, Available: p.key != ""}
}

func (p acoustIDPlugin) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if field != "recordingIdentity" {
		return nil, fault.Fail("INVALID_ARGUMENT", "unsupported AcoustID field")
	}
	if p.key == "" {
		return nil, fault.Fail("SOURCE_UNAVAILABLE", "AcoustID application key is missing")
	}
	fingerprint := strings.TrimSpace(q.Fingerprint)
	if len(fingerprint) == 0 || len(fingerprint) > 131072 || math.IsNaN(q.Duration) || math.IsInf(q.Duration, 0) || math.Round(q.Duration) < 1 || q.Duration > 2147483647 {
		return nil, fault.Fail("INVALID_ARGUMENT", "valid fingerprint and track duration are required")
	}
	params := url.Values{"client": {p.key}, "fingerprint": {fingerprint}, "duration": {strconv.FormatInt(int64(math.Round(q.Duration)), 10)},
		"meta": {"recordings"}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.acoustid.org/v2/lookup", strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := acoustPacer.wait(ctx); err != nil {
		return nil, err
	}
	var response struct {
		Status  string
		Results []struct {
			Score      float64
			Recordings []struct {
				ID, Title string
				Duration  float64
				Artists   []struct{ Name string }
			}
		}
	}
	if err := readCatalogJSON(p.client, req, &response); err != nil {
		return nil, err
	}
	if response.Status != "ok" {
		return nil, fault.Fail("SOURCE_UNAVAILABLE", "AcoustID lookup failed")
	}
	sort.SliceStable(response.Results, func(i, j int) bool { return response.Results[i].Score > response.Results[j].Score })
	var items []scraper.Item
	seen := make(map[string]bool)
	for _, result := range response.Results {
		for _, recording := range result.Recordings {
			if seen[recording.ID] || !brainzID.MatchString(recording.ID) {
				continue
			}
			title := recording.Title
			if title == "" {
				title = recording.ID
			}
			var artists []string
			for _, artist := range recording.Artists {
				if artist.Name != "" {
					artists = append(artists, artist.Name)
				}
			}
			seen[recording.ID] = true
			items = append(items, scraper.Item{ID: recording.ID, Title: title, Duration: recording.Duration, Artist: strings.Join(artists, ", "),
				Description: fmt.Sprintf("AcoustID score %.3f", result.Score), SourceURL: brainzSite + "/recording/" + recording.ID})
			if len(items) == 20 {
				return items, nil
			}
		}
	}
	return items, nil
}

package api_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineScraperModels(t *testing.T) {
	service, err := onlinescrape.New(online.Catalog(nil, ""))
	if err != nil {
		t.Fatal(err)
	}
	item := scraper.Item{ID: "edition", Title: "发行版本", Tracks: []scraper.Track{{
		Title: "CD2", Disc: 2, Number: 1, ExternalID: "track-20",
		LocalizedTitles:      []scraper.LocalizedTitle{{Language: "en", Title: "Finale"}},
		LocalizedArtistNames: []scraper.LocalizedTitle{{Language: "und", Title: "Rei Kagaya"}},
	}}}
	candidate := onlinescrape.Candidate{Item: item, Token: "token"}
	selected := works.ScrapedField{Field: "albumTracks", PluginID: "musicfs.scraper.musicbrainz", Item: item}
	for name, value := range map[string]any{
		"OnlineScrapers":              map[string]any{"items": service.List()},
		"OnlineScraperSearch":         map[string]any{"field": "artistImage", "query": scraper.Query{Artist: "歌手"}},
		"OnlineScraperCandidates":     map[string]any{"items": []onlinescrape.Candidate{candidate}},
		"WorkScrapedFields":           map[string]any{"items": []works.ScrapedField{selected}},
		"MusicAlbumScrapedFields":     map[string]any{"items": []works.ScrapedField{selected}},
		"MusicAlbumScrapedFieldInput": map[string]any{"expectedRevision": 1, "pluginId": "musicfs.scraper.musicbrainz", "candidateToken": "token"},
		"ScrapedMusicAlbumRevision":   map[string]any{"revision": 2},
	} {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, name, b)
	}
	albumFields := testkit.ContractSchema(t, "http", "MusicAlbumScrapedFields")
	if err := albumFields.Validate(testkit.DecodeJSON(t, []byte(`{"items":[{"field":"lyrics","pluginId":"fixture","item":{"id":"one","title":"Wrong field"}}]}`))); err == nil {
		t.Fatal("album fields accepted track lyrics")
	}
	schema := testkit.ContractSchema(t, "http", "OnlineScraperSearch")
	testkit.ValidateHTTP(t, "OnlineScraperSearch", []byte(`{"field":"albumDetails","query":{"externalId":"edition"},"refresh":true}`))
	for _, data := range []string{`{"field":"albumTracks","query":{}}`, `{"field":"unknown","query":{"title":"x"}}`, `{"field":"lyrics","query":{"country":"tw"}}`,
		`{"field":"lyrics","query":{"title":"x"},"refresh":true}`, `{"field":"albumTracks","query":{"title":"x"},"refresh":"true"}`} {
		if err := schema.Validate(testkit.DecodeJSON(t, []byte(data))); err == nil {
			t.Fatalf("invalid search accepted: %s", data)
		}
	}
}

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type autoAlbumCatalog struct {
	id    string
	items []scraper.Item
}

func (p autoAlbumCatalog) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{Type: "scraper", PluginID: "musicfs.scraper." + p.id, Name: p.id, Fields: []string{"albumDetails", "albumTracks"}, Available: true}
}

func (p autoAlbumCatalog) Search(_ context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if field == "albumTracks" && q.ExternalID != "" {
		track := scraper.Track{Title: "WAN", Artist: "Singer / Guest"}
		if q.ExternalID == "wrong-edition" {
			track = scraper.Track{Title: "Tryggvason", Artist: "YUN"}
		}
		return []scraper.Item{{ID: q.ExternalID, Title: "Edition", Tracks: []scraper.Track{track}}}, nil
	}
	if field != "albumDetails" || q.Album != "月光紀行" {
		return nil, errors.New("expected album name search")
	}
	return p.items, nil
}

type albumRankingFixture struct {
	music.Repository
	target music.AlbumScrapeTarget
}

func (f albumRankingFixture) MusicAlbumScrapeTarget(context.Context, string, string, string) (music.AlbumScrapeTarget, error) {
	return f.target, nil
}

func (albumRankingFixture) MusicAlbumTrackEvidence(context.Context, string, string, string) ([]music.AlbumTrackEvidence, error) {
	return []music.AlbumTrackEvidence{{Title: "WAN", ArtistNames: []string{"Singer"}}}, nil
}

func TestAlbumAutoScrapePrefersStrongerMatchesAcrossPlatforms(t *testing.T) {
	target := music.AlbumScrapeTarget{Name: "月光紀行", Artist: "Singer"}
	for _, tt := range []struct {
		name    string
		plugins []scraper.Plugin
		want    []string
	}{
		{"artist resolves same title", []scraper.Plugin{
			autoAlbumCatalog{"musicbrainz", []scraper.Item{{ID: "one", Title: target.Name, Artist: "Other"},
				{ID: "two", Title: target.Name, Artist: "Other"}, {ID: "three", Title: target.Name, Artist: "Other"}}},
			autoAlbumCatalog{"itunes", []scraper.Item{{ID: "right-artist", Title: target.Name, Artist: target.Artist}}},
		}, []string{"right-artist", "one", "two"}},
		{"near title beats weak overlap", []scraper.Plugin{
			autoAlbumCatalog{"musicbrainz", []scraper.Item{{ID: "distant", Title: "月光奏鳴曲"}, {ID: "unrelated", Title: "Overture"}}},
			autoAlbumCatalog{"itunes", []scraper.Item{{ID: "near", Title: "月光紀行集"}}},
		}, []string{"near", "distant"}},
		{"first platform unavailable", []scraper.Plugin{
			autoAlbumCatalog{"netease", []scraper.Item{{ID: "fallback", Title: target.Name}}},
		}, []string{"fallback"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service, err := onlinescrape.New(tt.plugins)
			if err != nil {
				t.Fatal(err)
			}
			items, err := service.SearchVerifiedAlbum(t.Context(), albumRankingFixture{target: target}, "admin", "album", "")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range items {
				got = append(got, item.ID)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("candidates %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAlbumAutoScrapeRanksRealSources(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := t.Context()
	service, err := onlinescrape.NewWithSettings([]scraper.Plugin{
		autoAlbumCatalog{"musicbrainz", []scraper.Item{
			{ID: "live", Title: "月光紀行 Live"}, {ID: "mb", Title: "月光紀行"}, {ID: "mb", Title: "月光紀行"},
		}},
		autoAlbumCatalog{"itunes", []scraper.Item{
			{ID: "alias", Title: "Moonlight Journey", LocalizedTitles: []scraper.LocalizedTitle{{Language: "zh", Title: "月光紀行"}}},
			{ID: "artist", Title: "月光紀行", Artist: "Singer"},
		}},
		autoAlbumCatalog{"netease", []scraper.Item{{ID: "ne", Title: "月光紀行"}}},
		autoAlbumCatalog{"qqmusic", []scraper.Item{{ID: "wrong-edition", Title: "月光紀行", Artist: "Singer"}, {ID: "qq", Title: "月光紀行"}}},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnlineAlbumScrapers(service, store)
	makeAlbum := func(instance, platform string) string {
		t.Helper()
		if err := store.BindMusicSourcePlugins(ctx, map[string]string{instance: "musicfs.plugin.source." + platform}); err != nil {
			t.Fatal(err)
		}
		plan := testkit.ScanPlan(instance)
		if _, _, err := store.Enqueue(ctx, "admin", instance, plan, "scan-"+platform); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		name := "月光紀行"
		title := "WAN"
		item := library.Observation{Observation: pluginproto.Observation{ExternalID: "one", RootID: instance, RelativePath: "one.wav",
			ObservedVersion: "v1", DisplayName: "one.wav", MIME: "audio/wav", SizeBytes: 30}, Title: &title, Artists: []string{"Singer"}, Album: &name}
		if err = store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{item})); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.MusicAlbums(ctx, "admin", music.Query{ProviderID: instance, Limit: 10})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("album from %s: %+v, %v", platform, page, err)
		}
		return page.Items[0].ID
	}
	const localInstance, qqInstance = "316664dc-7106-5acd-8df7-88c9d0d9baef", "d6bd77ed-c604-4afc-8ae7-8fd12b32e95d"
	local := makeAlbum(localInstance, "localfiles")
	netease := makeAlbum("843558e1-a158-56ac-834a-baffd12bb5c5", "netease")
	qq := makeAlbum(qqInstance, "qqmusic")
	call := func(id, body string, status int, want ...string) []onlinescrape.AutoAlbumCandidate {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/library/albums/"+id+"/auto-scrape", strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("auto scrape: %d %s", w.Code, w.Body.String())
		}
		if status != 200 {
			return nil
		}
		testkit.ValidateHTTP(t, "AlbumAutoSearchInput", []byte(body))
		testkit.ValidateHTTP(t, "RankedAlbumCandidates", w.Body.Bytes())
		var page struct {
			Items []onlinescrape.AutoAlbumCandidate
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, item := range page.Items {
			selection, err := service.Candidate("admin", item.PluginID, "albumDetails", item.Token)
			if err != nil || selection.Item.ID != item.ID {
				t.Fatal("ranked candidate cannot be selected", err)
			}
			trackList, err := service.Candidate("admin", item.PluginID, "albumTracks", item.AlbumTracksToken)
			if err != nil || trackList.Item.ID != item.ID || len(trackList.Item.Tracks) == 0 {
				t.Fatal("verified album lacks its paired track list", err)
			}
			got = append(got, item.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("candidates %v, want %v", got, want)
		}
		return page.Items
	}
	call(local, "{}", 200, "artist", "mb", "alias")
	call(netease, "{}", 200, "ne")
	call(qq, `{"providerInstanceId":"`+qqInstance+`"}`, 200, "qq")
	call(qq, `{"providerInstanceId":"`+localInstance+`"}`, 404)
	call(qq, `{"providerInstanceId":"invalid"}`, 400)
	other, err := store.SaveMusicAlbum(ctx, "other", "", 0, music.AlbumFields{Name: "Private"}, "other-album")
	if err != nil {
		t.Fatal(err)
	}
	call(other.ID, "{}", 404)
	rev := int64(0)
	_, err = service.SaveAlbumAutoSettings(ctx, "admin", onlinescrape.AlbumAutoSettingsInput{
		ExpectedRevision: &rev, LocalPluginOrder: []string{"musicfs.scraper.netease", "musicfs.scraper.musicbrainz"},
	}, "auto-order")
	if err != nil {
		t.Fatal(err)
	}
	call(local, "{}", 200, "ne", "mb", "live")
	verified := call(qq, "{}", 200, "qq")[0]
	current, err := store.MusicAlbum(ctx, "admin", qq)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"expectedRevision": current.Revision, "pluginId": verified.PluginID, "providerInstanceId": qqInstance,
		"candidates": map[string]string{"albumDetails": verified.Token, "albumTracks": verified.AlbumTracksToken}}
	raw, _ := json.Marshal(input)
	testkit.ValidateHTTP(t, "AutoAlbumSelectionInput", raw)
	for range 2 {
		r := httptest.NewRequest("PUT", "/api/v1/library/albums/"+qq+"/auto-scrape", strings.NewReader(string(raw)))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Idempotency-Key", "auto-save-verified-edition")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("verified selection: %d %s", w.Code, w.Body.String())
		}
	}
}

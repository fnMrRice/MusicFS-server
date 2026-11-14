package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type lyricsTransport func(*http.Request) (*http.Response, error)

func (f lyricsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnlineScraperTrackLyricsSelectionAndReplay(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := t.Context()
	provider := fixtureInstanceID("admin")
	if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan(provider), "scraper-lyrics-seed"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	observation := library.Observation{Observation: pluginproto.Observation{ExternalID: "source", RootID: provider, RelativePath: "song.wav", DisplayName: "song", ObservedVersion: "version-1", SizeBytes: 1}, Artists: []string{"Singer"}}
	if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{observation})); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, job, 1, nil); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.ListTracks(ctx, "admin", provider, "", 1)
	if err != nil || len(tracks.Items) != 1 {
		t.Fatalf("missing track: %v", err)
	}
	track := tracks.Items[0]
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	media := mediastore.NewContent(store, store, files, thumbnail.New())
	server.RegisterMedia(media)
	const lyrics = "[00:01.00]Synthetic lyric"
	const plugin = "musicfs.scraper.lrclib"
	searches, downloads := 0, 0
	client := &http.Client{Transport: lyricsTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.bgm.tv" && r.URL.Path == "/v0/subjects/40" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":40,"type":3,"name":"Album","images":{"large":"https://lain.bgm.tv/pic/cover/l/preview.png"}}`)), Header: make(http.Header)}, nil
		}
		if r.URL.Host == "lain.bgm.tv" && r.URL.Path == "/pic/cover/l/preview.png" {
			downloads++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(testkit.ScraperPNG())), Header: http.Header{"Content-Type": []string{"image/png"}}}, nil
		}
		searches++
		if r.Method != "GET" || r.URL.Host != "lrclib.net" || r.URL.Path != "/api/get" || r.URL.Query().Get("track_name") != "One" {
			t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL)
		}
		response := fmt.Sprintf(`{"id":37,"trackName":"One","artistName":"Singer","syncedLyrics":%q}`, lyrics)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
	})}
	catalog, err := onlinescrape.New([]scraper.Plugin{online.LRCLIB(client), online.Bangumi(client)})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnlineScrapers(catalog, store, media, client)
	request := func(method, path, key string, input any, want int) []byte {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var candidates struct{ Items []onlinescrape.Candidate }
	search := request("POST", "/scrapers/"+plugin+"/search", "", map[string]any{"field": "lyrics", "query": scraper.Query{Title: "One", Artist: "Singer"}}, 200)
	if err := json.Unmarshal(search, &candidates); err != nil || len(candidates.Items) != 1 || candidates.Items[0].Token == "" || candidates.Items[0].Lyrics != lyrics {
		t.Fatalf("invalid candidates: %s", search)
	}
	base := "/source-tracks/" + track.ID
	var selection mediastore.Selection
	before := request("GET", base+"/media-selection", "", nil, 200)
	if err := json.Unmarshal(before, &selection); err != nil || selection.Revision != 0 || len(selection.Bindings) != 0 {
		t.Fatalf("search unexpectedly selected content: %s", before)
	}
	input := map[string]any{"pluginId": plugin, "candidateToken": candidates.Items[0].Token, "expectedRevision": 0, "assetId": track.AssetID, "assetVersion": track.Asset.Version}
	for i := 0; i < 2; i++ {
		got := request("PUT", base+"/scraped-fields/lyrics", "apply-online-lyrics", input, 200)
		if err := json.Unmarshal(got, &selection); err != nil || selection.Revision != 1 || len(selection.Bindings) != 1 || !selection.Bindings[0].ManualLocked {
			t.Fatalf("invalid manual selection: %s", got)
		}
	}
	request("PUT", base+"/scraped-fields/lyrics", "stale-online-lyrics", input, 409)
	persisted := request("GET", base+"/media-selection", "", nil, 200)
	if err := json.Unmarshal(persisted, &selection); err != nil || selection.Revision != 1 || len(selection.Bindings) != 1 || !selection.Bindings[0].Current || !selection.Bindings[0].ManualLocked {
		t.Fatalf("invalid persisted selection: %s", persisted)
	}
	objectPath := "/media/" + selection.Bindings[0].ObjectID
	if body := request("GET", objectPath, "", nil, 200); string(body) != lyrics || searches != 1 {
		t.Fatalf("unexpected body or lookup count: %s, %d", body, searches)
	}
	var info mediastore.ContentObject
	body := request("GET", objectPath+"/info", "", nil, 200)
	if err := json.Unmarshal(body, &info); err != nil || info.Metadata.Format != "lrc" || info.Metadata.Origin.Kind != "scraper" || info.Metadata.Origin.InstanceID != plugin || info.Metadata.Origin.CandidateID != "37" {
		t.Fatalf("invalid online provenance: %s", body)
	}
	const imagePlugin = "musicfs.scraper.bangumi"
	search = request("POST", "/scrapers/"+imagePlugin+"/search", "", map[string]any{"field": "artwork", "query": scraper.Query{ExternalID: "40"}}, 200)
	if err := json.Unmarshal(search, &candidates); err != nil || len(candidates.Items) != 1 {
		t.Fatalf("invalid artwork candidates: %s", search)
	}
	art := map[string]any{"pluginId": imagePlugin, "candidateToken": candidates.Items[0].Token, "expectedRevision": 0, "assetId": track.AssetID, "assetVersion": track.Asset.Version}
	request("PUT", base+"/scraped-fields/artwork", "stale-artwork-key", art, 409)
	if downloads != 0 {
		t.Fatal("stale selection downloaded artwork")
	}
	art["expectedRevision"] = 1
	first := request("PUT", base+"/scraped-fields/artwork", "apply-artwork-key", art, 200)
	replay := request("PUT", base+"/scraped-fields/artwork", "apply-artwork-key", art, 200)
	if !bytes.Equal(first, replay) || downloads != 1 {
		t.Fatalf("artwork replay downloaded or changed the result: %d", downloads)
	}
	art["candidateToken"] = "another-preview-token"
	request("PUT", base+"/scraped-fields/artwork", "apply-artwork-key", art, 409)
	// 重启后候选缓存清空，已提交的操作从持久化记录重放。
	fresh, err := onlinescrape.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server, _ = testServer()
	cookie, session = login(t, server)
	server.RegisterOnlineScrapers(fresh, store, media, client)
	replay = request("PUT", base+"/scraped-fields/lyrics", "apply-online-lyrics", input, 200)
	if err := json.Unmarshal(replay, &selection); err != nil || selection.Revision != 1 || len(selection.Bindings) != 1 || !selection.Bindings[0].ManualLocked {
		t.Fatalf("restart failed to replay the original selection: %s", replay)
	}
}

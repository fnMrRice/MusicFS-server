package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type lyricsCatalog struct {
	id    string
	calls *[]string
	items []scraper.Item
}

func TestLyricsCatalogProvenanceContract(t *testing.T) {
	const plugin = "musicfs.scraper.qqmusic"
	target := enrichment.Target{SourceTrackID: "source", AssetID: "asset", AssetVersion: "version"}
	candidate := enrichment.PreviewCandidate{CandidateID: domain.NewID(), Target: target, InstanceID: plugin, Field: "lyrics", Language: "und", Format: "lrc", MIME: "text/plain; charset=utf-8", SHA256: strings.Repeat("a", 64), Origin: "catalog", Basis: "matchedTrack", Status: "available", ExpiresAt: time.Now().UTC()}
	for name, value := range map[string]any{
		"MetadataCandidate": candidate,
		"MetadataIssue":     enrichment.JobIssue{InstanceID: plugin, Code: "RATE_LIMITED"},
		"ContentOrigin":     mediastore.Origin{Kind: "scraper", InstanceID: plugin, CandidateID: candidate.CandidateID},
	} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, name, raw)
	}
	for _, change := range []func(*enrichment.PreviewCandidate){
		func(c *enrichment.PreviewCandidate) { c.InstanceID = "musicfs.scraper.itunes" },
		func(c *enrichment.PreviewCandidate) { c.Field = "artwork" },
		func(c *enrichment.PreviewCandidate) { c.Origin = "sidecar" },
		func(c *enrichment.PreviewCandidate) { c.Basis = "exactStem" },
	} {
		wrong := candidate
		change(&wrong)
		raw, err := json.Marshal(wrong)
		if err != nil {
			t.Fatal(err)
		}
		if testkit.ContractSchema(t, "http", "MetadataCandidate").Validate(testkit.DecodeJSON(t, raw)) == nil {
			t.Fatal("invalid catalog provenance accepted", wrong)
		}
	}
}

func (p lyricsCatalog) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{Type: "scraper", PluginID: p.id, Name: p.id, Available: true, Fields: []string{"lyrics"}}
}

func (p lyricsCatalog) Search(_ context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	*p.calls = append(*p.calls, p.id)
	if field != "lyrics" || q.ExternalID != "" || q.Title != "One" {
		return nil, errors.New("unexpected cross-source query")
	}
	return p.items, nil
}

func TestLyricsAutoSettingsAndOrderedSearch(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	const local, netease, qq, lrc = onlinescrape.LocalLyricsPluginID, "musicfs.scraper.netease", "musicfs.scraper.qqmusic", "musicfs.scraper.lrclib"
	var calls []string
	right := scraper.Item{ID: "right", Title: "One", Artist: "First / Second", Lyrics: "[00:01]Test"}
	catalog, err := onlinescrape.NewWithSettings([]scraper.Plugin{
		lyricsCatalog{netease, &calls, []scraper.Item{{ID: "wrong", Title: "One", Artist: "Other", Lyrics: "Wrong track"}}},
		lyricsCatalog{qq, &calls, []scraper.Item{right}}, lyricsCatalog{lrc, &calls, []scraper.Item{right}},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	localBody := false
	catalog.SetLocalLyricsLookup(func(_ context.Context, owner, source string, _ scraper.Query) (onlinescrape.LocalLyricsResult, error) {
		calls = append(calls, local)
		if owner != "admin" || source != "track" {
			return onlinescrape.LocalLyricsResult{}, errors.New("unexpected track")
		}
		if localBody {
			return onlinescrape.LocalLyricsResult{AssetID: "asset", AssetVersion: "version", Items: []scraper.Item{right}}, nil
		}
		return onlinescrape.LocalLyricsResult{}, nil
	})
	server.RegisterOnlineScrapers(catalog, store, nil, http.DefaultClient)
	call := func(method, path, body, key string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/scrapers/"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s returned %d: %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	settings := func(raw []byte) onlinescrape.LyricsAutoSettings {
		t.Helper()
		testkit.ValidateHTTP(t, "LyricsAutoSettings", raw)
		var page onlinescrape.LyricsAutoSettings
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	const path = "lyrics-auto-settings"
	initial := settings(call("GET", path, "", "", 200))
	if initial.Revision != 0 || !slices.Equal(initial.PluginOrder, []string{local, netease, qq}) {
		t.Fatal(initial)
	}
	search := func(localOnly bool) onlinescrape.LyricsAutoResult {
		t.Helper()
		input, err := json.Marshal(onlinescrape.LyricsAutoSearchInput{Query: scraper.Query{Title: "One", Artist: "First, Second", ExternalID: "platform-specific"}, SourceTrackID: "track", LocalOnly: localOnly})
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, "LyricsAutoSearchInput", input)
		raw := call("POST", "lyrics-auto-search", string(input), "", 200)
		testkit.ValidateHTTP(t, "LyricsAutoResult", raw)
		var result onlinescrape.LyricsAutoResult
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	found := search(false)
	if found.PluginID != qq || len(found.Items) != 1 || found.Items[0].Token == "" || !slices.Equal(calls, []string{local, netease, qq}) {
		t.Fatal(found, calls)
	}
	const ordered = `{"expectedRevision":0,"pluginOrder":["musicfs.scraper.lrclib","musicfs.scraper.local"]}`
	testkit.ValidateHTTP(t, "LyricsAutoSettingsInput", []byte(ordered))
	settings(call("PUT", path, ordered, "lyrics-order", 200))
	settings(call("PUT", path, ordered, "lyrics-order", 200))
	call("PUT", path, ordered, "lyrics-stale", 409)
	for _, list := range []string{`["unknown"]`, `["musicfs.scraper.local","musicfs.scraper.local"]`, `null`} {
		call("PUT", path, `{"expectedRevision":1,"pluginOrder":`+list+`}`, "lyrics-invalid", 400)
	}
	calls = nil
	if found := search(false); found.PluginID != lrc || !slices.Equal(calls, []string{lrc}) {
		t.Fatal(found, calls)
	}
	disabled := settings(call("PUT", path, `{"expectedRevision":1,"pluginOrder":[]}`, "disable-lyrics", 200))
	if disabled.PluginOrder == nil || len(disabled.PluginOrder) != 0 || disabled.Revision != 2 {
		t.Fatal(disabled)
	}
	calls = nil
	if found := search(false); found.PluginID != "" || len(found.Items) != 0 || len(calls) != 0 {
		t.Fatal(found, calls)
	}
	localBody = true
	found = search(true)
	if found.PluginID != local || len(found.Items) != 1 || !slices.Equal(calls, []string{local}) {
		t.Fatal(found, calls)
	}
	target := mediastore.ContentTarget{SourceTrackID: "track", AssetID: "asset", AssetVersion: "version"}
	token := found.Items[0].Token
	if _, err = catalog.CandidateForTrack("admin", local, "lyrics", token, target); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []mediastore.ContentTarget{{SourceTrackID: "another", AssetID: "asset", AssetVersion: "version"}, {SourceTrackID: "track", AssetID: "asset", AssetVersion: "new"}} {
		if _, err = catalog.CandidateForTrack("admin", local, "lyrics", token, bad); err == nil {
			t.Fatal("accepted stale or unrelated candidate")
		}
	}
	if _, err = catalog.Candidate("admin", local, "lyrics", token); err == nil {
		t.Fatal("local candidate usable without track")
	}
	if _, err = catalog.CandidateForTrack("other", local, "lyrics", token, target); err == nil {
		t.Fatal("local candidate crossed owner")
	}
	other, err := catalog.LyricsAutoSettings(t.Context(), "other")
	if err != nil || !slices.Equal(other.PluginOrder, initial.PluginOrder) {
		t.Fatal(other, err)
	}
}

func TestLyricsAutoFallbackAndCancellation(t *testing.T) {
	catalog, err := onlinescrape.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	limited := domain.Fail("RATE_LIMITED", "upstream cooldown")
	err = catalog.VisitLyricsSources(t.Context(), "owner", func(_ context.Context, _ string) (bool, error) {
		calls++
		if calls == 1 {
			return false, limited
		}
		return false, nil
	})
	if !errors.Is(err, limited) || calls != 3 {
		t.Fatal(err, calls)
	}
	calls = 0
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err = catalog.VisitLyricsSources(ctx, "owner", func(_ context.Context, _ string) (bool, error) {
		calls++
		cancel()
		return false, limited
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(err, calls)
	}
}

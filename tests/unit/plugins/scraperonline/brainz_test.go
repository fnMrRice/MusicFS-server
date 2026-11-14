package scraperonline_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

const releaseID = "f58a2ce8-60e6-4129-8bea-024bd5c26b74"
const recordingID = "717c7e58-b1f2-45ab-8816-eaa82aaa37c8"

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 保留插件请求的主机及参数，在内存中检查 HTTP 契约和响应解析。
func catalogClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	return &http.Client{Transport: catalogTransport(func(r *http.Request) (*http.Response, error) {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out.Result(), nil
	})}
}

func TestReleaseTracksSearchAndLookup(t *testing.T) {
	client := catalogClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "MusicFS") || r.Host != "musicbrainz.org" || r.URL.Query().Get("fmt") != "json" {
			t.Errorf("unexpected upstream request: %v", r)
		}
		switch r.URL.Path {
		case "/ws/2/release":
			if got := r.URL.Query().Get("query"); got != `release:"A \"Test\"" AND artist:"乐团" AND country:"TW"` {
				t.Errorf("unexpected query: %q", got)
			}
			fmt.Fprintf(w, `{"releases":[{"id":"%s","title":"A Test"}]}`, releaseID)
		case "/ws/2/release/" + releaseID:
			if !strings.Contains(r.URL.Query().Get("inc"), "recordings") {
				t.Errorf("missing recordings include: %v", r.URL)
			}
			fmt.Fprintf(w, `{"id":"%s","title":"A Test","date":"2025-02","media":[
			  {"position":1,"tracks":[{"position":1,"title":"First","length":12050}]},
			  {"position":2,"tracks":[{"position":1,"recording":{"title":"Second","length":9000,"artist-credit":[{"name":"B","joinphrase":" & "},{"artist":{"name":"C"}}]}}]}]}`, releaseID)
		default:
			http.NotFound(w, r)
		}
	})
	items, err := online.MusicBrainz(client).Search(context.Background(), "albumTracks", scraper.Query{Album: `A "Test"`, Artist: "乐团", Country: "TW"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 {
		t.Fatalf("unexpected track list: %#v %v", items, err)
	}
	if first, second := items[0].Tracks[0], items[0].Tracks[1]; first.Duration != 12.05 || second.Disc != 2 || second.Number != 1 || second.Title != "Second" || second.Artist != "B & C" {
		t.Fatalf("unexpected track fields: %#v", items[0].Tracks)
	}
}

func TestCoverArchiveKeepsOnlyAvailableFront(t *testing.T) {
	client := catalogClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("unexpected cover request: %v", r)
		}
		switch r.URL.Host {
		case "coverartarchive.org":
			if r.URL.Path == "/release/"+releaseID+"/front-500" {
				http.Redirect(w, r, "https://ia800000.us.archive.org/cover.jpg", http.StatusTemporaryRedirect)
			} else {
				http.NotFound(w, r)
			}
		case "ia800000.us.archive.org":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected image host: %s", r.URL.Host)
		}
	})
	plugin := online.CoverArtArchive(client)
	items, err := plugin.Search(context.Background(), "artwork", scraper.Query{ExternalID: releaseID})
	if err != nil || len(items) != 1 || items[0].ImageURL != "https://coverartarchive.org/release/"+releaseID+"/front-500" {
		t.Fatalf("unexpected cover result: %#v %v", items, err)
	}
	items, err = plugin.Search(context.Background(), "artwork", scraper.Query{ExternalID: recordingID})
	if err != nil || len(items) != 0 {
		t.Fatalf("missing cover: %#v %v", items, err)
	}
	if _, err := plugin.Search(context.Background(), "artwork", scraper.Query{ExternalID: "not a release ID"}); err == nil {
		t.Fatal("invalid release ID accepted")
	}
}

func TestAcoustIDFingerprintResult(t *testing.T) {
	client := catalogClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.acoustid.org" || r.URL.Path != "/v2/lookup" || r.URL.RawQuery != "" || r.Method != http.MethodPost {
			t.Errorf("unexpected lookup: %v", r)
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("duration") != "201" || r.PostForm.Get("fingerprint") != "fingerprint" || r.PostForm.Get("client") != "test-key" {
			t.Errorf("unexpected form: %v %v", r.PostForm, err)
		}
		fmt.Fprintf(w, `{"status":"ok","results":[{"score":0.5,"recordings":[{"id":"%s","title":"Other"}]},{"score":0.9,"recordings":[{"id":"%s","title":"Best","duration":201,"artists":[{"name":"A"}]}]}]}`, recordingID, recordingID)
	})
	p := online.AcoustID(client, "test-key")
	items, err := p.Search(context.Background(), "recordingIdentity", scraper.Query{Fingerprint: "fingerprint", Duration: 200.7})
	if err != nil || len(items) != 1 || items[0].ID != recordingID || items[0].Title != "Best" || items[0].Artist != "A" {
		t.Fatalf("unexpected identities: %#v %v", items, err)
	}
	if online.AcoustID(client, "").Descriptor().Available {
		t.Fatal("missing key reported as available")
	}
}

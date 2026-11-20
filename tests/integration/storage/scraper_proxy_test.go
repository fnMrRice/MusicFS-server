package storage_test

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

const proxyReleaseID = "f58a2ce8-60e6-4129-8bea-024bd5c26b74"

func newProxyServices(t *testing.T, store settings.Repository, client *http.Client) (*onlinescrape.Service, *settings.Service) {
	t.Helper()
	service, err := onlinescrape.NewWithSettings(online.Catalog(client, ""), store)
	if err != nil {
		t.Fatal(err)
	}
	return service, settings.New(store, service.SettingsProviders()...)
}

func saveProxy(t *testing.T, service *settings.Service, owner, id, mode, address string) {
	t.Helper()
	page, err := service.Read(t.Context(), owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(t.Context(), owner, id, settings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"proxyMode": mode, "proxyUrl": address}}); err != nil {
		t.Fatal(err)
	}
}

func proxyLyrics(w http.ResponseWriter, r *http.Request) {
	if r.Host != "lrclib.net" || r.URL.Path != "/api/get/1" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":1,"trackName":"Synthetic song","plainLyrics":"Synthetic lyric"}`)
}

func TestScraperProxyRealProtocolsDirectAndPersistedIsolation(t *testing.T) {
	network := newScraperProxyNetwork(t, proxyLyrics)
	path := filepath.Join(t.TempDir(), "proxy.sqlite")
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	service, configuration := newProxyServices(t, store, network.client)
	const id = "musicfs.scraper.lrclib"
	search := func(owner string) {
		t.Helper()
		items, err := service.Search(t.Context(), owner, id, "lyrics", scraper.Query{ExternalID: "1"})
		if err != nil || len(items) != 1 || items[0].Lyrics != "Synthetic lyric" {
			t.Fatalf("proxy lookup failed: %v %#v", err, items)
		}
	}
	for _, test := range []struct{ route, address string }{{"http", network.httpURL}, {"https", network.httpsURL}, {"socks", network.socksURL}, {"socks", network.socksHURL()}} {
		before := network.count(test.route, "lrclib.net:443")
		saveProxy(t, configuration, "admin", id, "custom", test.address)
		search("admin")
		if network.count(test.route, "lrclib.net:443") != before+1 {
			t.Fatal("configured proxy did not carry the lookup", test.route)
		}
	}
	// 已保存的配置跨用户、跨插件隔离；无效新配置不能覆盖之前的代理。
	saveProxy(t, configuration, "admin", id, "custom", network.httpURL)
	saveProxy(t, configuration, "other", id, "direct", "")
	saveProxy(t, configuration, "admin", "musicfs.scraper.bangumi", "direct", "")
	page, _ := configuration.Read(t.Context(), "admin", id)
	for _, values := range []settings.Values{{"proxyMode": "custom", "proxyUrl": ""}, {"proxyMode": "custom", "proxyUrl": "http://user:secret@proxy:80"}} {
		_, err = configuration.Update(t.Context(), "admin", id, settings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.Schema.Revision, Values: values})
		matchingError(t, err, "INVALID_ARGUMENT")
	}
	unchanged, err := configuration.Read(t.Context(), "admin", id)
	if err != nil || unchanged.Revision != page.Revision || unchanged.Values["proxyUrl"] != network.httpURL {
		t.Fatal("invalid configuration changed saved values", err)
	}
	before := network.count("http", "lrclib.net:443")
	search("other")
	if network.count("direct", "lrclib.net:443") != 1 || network.count("http", "lrclib.net:443") != before {
		t.Fatal("proxy crossed user boundary")
	}
	otherPlugin, _ := configuration.Read(t.Context(), "admin", "musicfs.scraper.bangumi")
	if otherPlugin.Values["proxyMode"] != "direct" {
		t.Fatal("proxy crossed plugin boundary")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	service, configuration = newProxyServices(t, store, network.client)
	search("admin")
	if network.count("http", "lrclib.net:443") != before+1 {
		t.Fatal("restarted service lost proxy settings")
	}
	// 保存直连后下一次查询立即生效，也不改变原始客户端的 Transport。
	saveProxy(t, configuration, "admin", id, "direct", "")
	search("admin")
	if network.count("direct", "lrclib.net:443") != 2 || network.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("direct mode did not replace saved proxy without mutating base client")
	}
}

func TestCoverArchiveUsesOneProxyForBrainzArtworkAndAllowedRedirects(t *testing.T) {
	network := newScraperProxyNetwork(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "musicbrainz.org":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"releases":[{"id":"%s","title":"Synthetic album"}]}`, proxyReleaseID)
		case "coverartarchive.org":
			if r.URL.Path == "/blocked" {
				http.Redirect(w, r, "https://untrusted.invalid/image", http.StatusTemporaryRedirect)
				return
			}
			http.Redirect(w, r, "https://ia800000.us.archive.org/image", http.StatusTemporaryRedirect)
		case "ia800000.us.archive.org":
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "synthetic image bytes")
		default:
			t.Errorf("unexpected upstream host: %s", r.Host)
			http.NotFound(w, r)
		}
	})
	store := evidenceStore(t)
	service, configuration := newProxyServices(t, store, network.client)
	const id = "musicfs.scraper.coverartarchive"
	saveProxy(t, configuration, "admin", id, "custom", network.httpURL)
	items, err := service.Search(t.Context(), "admin", id, "artwork", scraper.Query{Album: "Synthetic album"})
	if err != nil || len(items) != 1 {
		t.Fatalf("cover lookup failed: %#v, %v", items, err)
	}
	for _, host := range []string{"musicbrainz.org:443", "coverartarchive.org:443", "ia800000.us.archive.org:443"} {
		if network.count("http", host) != 1 || network.count("direct", host) != 0 {
			t.Fatal("cross-site cover lookup bypassed proxy", host)
		}
	}
	selected := works.ScrapedField{Field: "artwork", PluginID: id, Item: items[0].Item}
	body, contentType, err := service.OpenMedia(t.Context(), "admin", selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || contentType != "image/png" || string(raw) != "synthetic image bytes" || network.count("http", "ia800000.us.archive.org:443") != 2 {
		t.Fatal("artwork download or redirect bypassed plugin proxy", err)
	}
	selected.Item.ImageURL = "https://coverartarchive.org/blocked"
	if body, _, err = service.OpenMedia(t.Context(), "admin", selected, nil); err == nil {
		body.Close()
		t.Fatal("untrusted artwork redirect accepted")
	}
	if network.count("http", "untrusted.invalid:443") != 0 {
		t.Fatal("untrusted redirect reached proxy")
	}
	// 另一个插件的独立设置不会被 Cover Art Archive 的代理影响。
	saveProxy(t, configuration, "admin", "musicfs.scraper.musicbrainz", "direct", "")
	if _, err := service.Search(t.Context(), "admin", "musicfs.scraper.musicbrainz", "albumDetails", scraper.Query{Album: "Synthetic album"}); err != nil {
		t.Fatal(err)
	}
	if network.count("direct", "musicbrainz.org:443") != 1 {
		t.Fatal("proxy crossed plugin boundary")
	}
}

func TestScraperProxyConcurrentOwnersKeepTheirTransports(t *testing.T) {
	network := newScraperProxyNetwork(t, proxyLyrics)
	service, configuration := newProxyServices(t, evidenceStore(t), network.client)
	const id = "musicfs.scraper.lrclib"
	owners := []string{"proxy-one", "direct-one", "proxy-two", "direct-two"}
	for _, owner := range owners {
		mode, address := "direct", ""
		if strings.HasPrefix(owner, "proxy") {
			mode, address = "custom", network.httpURL
		}
		saveProxy(t, configuration, owner, id, mode, address)
	}
	var wg sync.WaitGroup
	for _, owner := range owners {
		wg.Go(func() {
			items, err := service.Search(t.Context(), owner, id, "lyrics", scraper.Query{ExternalID: "1"})
			if err != nil || len(items) != 1 {
				t.Errorf("concurrent query failed: %v", err)
			}
		})
	}
	wg.Wait()
	if network.count("http", "lrclib.net:443") != 2 || network.count("direct", "lrclib.net:443") != 2 {
		t.Fatal("concurrent requests shared the wrong proxy configuration")
	}
}

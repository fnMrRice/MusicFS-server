package scraperonline_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestEveryCatalogScraperDeclaresProxySettings(t *testing.T) {
	for _, plugin := range online.Catalog(nil, "") {
		t.Run(plugin.Descriptor().PluginID, func(t *testing.T) {
			provider, ok := plugin.(scraper.SettingsProvider)
			if !ok {
				t.Fatal("scraper does not provide settings")
			}
			schema := provider.SettingsSchema()
			values := settings.Defaults(schema)
			if schema.Revision != "scraper-proxy-v1" || len(schema.Fields) != 2 || values["proxyMode"] != "environment" || values["proxyUrl"] != "" {
				t.Fatalf("invalid proxy schema: %#v", schema)
			}
			if err := provider.ValidateSettings(values); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProxySettingsRejectInvalidOrCredentialedAddressesWithoutEcho(t *testing.T) {
	provider := online.Catalog(nil, "")[0].(scraper.SettingsProvider)
	for _, raw := range []string{"", "localhost:7890", "ftp://proxy:21", "http://user:secret@proxy:7890", "http://proxy:7890/path", "http://proxy:7890?secret=value", "http://proxy/#fragment", "http://proxy:0", "http://proxy:65536", "http://proxy:", "http://proxy:abc", "http://", "http://proxy\n"} {
		if err := provider.ValidateSettings(map[string]any{"proxyMode": "custom", "proxyUrl": raw}); err == nil || strings.Contains(err.Error(), "secret") || raw != "" && strings.Contains(err.Error(), raw) {
			t.Fatalf("invalid address accepted or echoed: %q, %v", raw, err)
		}
	}
	for _, mode := range []any{"invalid", nil, 7} {
		if err := provider.ValidateSettings(map[string]any{"proxyMode": mode, "proxyUrl": ""}); err == nil {
			t.Fatal("invalid mode accepted")
		}
	}
}

func TestConfiguredClientsCloneTransportAndPreserveBase(t *testing.T) {
	baseProxy, _ := url.Parse("http://base.invalid:9000")
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.Proxy = http.ProxyURL(baseProxy)
	base := &http.Client{Transport: baseTransport}
	provider := online.Catalog(base, "")[0].(scraper.SettingsProvider)
	req, _ := http.NewRequest("GET", "https://musicbrainz.org/", nil)
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		address := scheme + "://127.0.0.1:7890"
		_, client, err := provider.WithSettings(map[string]any{"proxyMode": "custom", "proxyUrl": address})
		if err != nil {
			t.Fatal(err)
		}
		transport := client.Transport.(*http.Transport)
		proxy, err := transport.Proxy(req)
		if err != nil || proxy.String() != address || transport == baseTransport || client == base {
			t.Fatalf("proxy did not get independent transport: %v, %v", proxy, err)
		}
	}
	_, direct, err := provider.WithSettings(map[string]any{"proxyMode": "direct", "proxyUrl": ""})
	if err != nil || direct.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("direct mode retained a proxy", err)
	}
	_, environment, err := provider.WithSettings(map[string]any{"proxyMode": "environment", "proxyUrl": ""})
	if err != nil {
		t.Fatal(err)
	}
	got, gotErr := environment.Transport.(*http.Transport).Proxy(req)
	want, wantErr := http.ProxyFromEnvironment(req)
	if (got == nil) != (want == nil) || got != nil && got.String() != want.String() || (gotErr == nil) != (wantErr == nil) {
		t.Fatal("environment mode does not follow Go server proxy environment")
	}
	if proxy, _ := baseTransport.Proxy(req); proxy.String() != baseProxy.String() || http.DefaultTransport == direct.Transport {
		t.Fatal("configuration changed shared transport")
	}
}

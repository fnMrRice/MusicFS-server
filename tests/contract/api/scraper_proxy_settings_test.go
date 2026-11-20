package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScraperProxySettingsHTTPSchemaAuthorizationRevisionAndIsolation(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	scrapers, err := onlinescrape.NewWithSettings(online.Catalog(nil, ""), store)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterPluginSettings(settings.New(store, scrapers.SettingsProviders()...))
	manager, err := plugincontrol.New(t.Context(), config.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	server.RegisterPluginManagement(manager)
	request := func(method, path string, input any, authenticated, csrf bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if authenticated {
			r.AddCookie(cookie)
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "proxy-configuration-save")
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	const path = "/api/v1/plugin-settings/musicfs.scraper.bangumi"
	read := request("GET", path, nil, true, false)
	if read.Code != 200 {
		t.Fatal(read.Body.String())
	}
	testkit.ValidateHTTP(t, "PluginSettingsPage", read.Body.Bytes())
	var page settings.Page
	if err := json.Unmarshal(read.Body.Bytes(), &page); err != nil || page.Schema.Revision != "scraper-proxy-v1" || page.Revision != 0 || page.Values["proxyMode"] != "environment" {
		t.Fatal("incorrect scraper settings declaration", err)
	}
	expectedRevision := page.Revision
	valid := settings.Request{ExpectedRevision: &expectedRevision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"proxyMode": "custom", "proxyUrl": "http://127.0.0.1:17890"}}
	if w := request("PUT", path, valid, true, false); w.Code != 403 {
		t.Fatal("scraper settings CSRF bypass")
	}
	if w := request("GET", path, nil, false, false); w.Code != 401 {
		t.Fatal("anonymous scraper settings exposed")
	}
	for _, values := range []settings.Values{{"proxyMode": "custom", "proxyUrl": ""}, {"proxyMode": "custom", "proxyUrl": "http://user:secret@proxy:7890"}, {"proxyMode": "custom", "proxyUrl": "file:///private"}, {"proxyMode": "custom", "proxyUrl": "http://proxy/path"}, {"proxyMode": "bad", "proxyUrl": ""}, {"proxyMode": "direct", "unknown": true}} {
		invalid := valid
		invalid.Values = values
		w := request("PUT", path, invalid, true, true)
		if w.Code != 400 || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("invalid proxy accepted or credential echoed: %s", w.Body.String())
		}
	}
	invalidSchema := valid
	invalidSchema.SchemaRevision = "stale-schema"
	if w := request("PUT", path, invalidSchema, true, true); w.Code != 400 {
		t.Fatal("stale settings schema accepted")
	}
	saved := request("PUT", path, valid, true, true)
	if saved.Code != 200 {
		t.Fatal(saved.Body.String())
	}
	testkit.ValidateHTTP(t, "PluginSettingsPage", saved.Body.Bytes())
	if err := json.Unmarshal(saved.Body.Bytes(), &page); err != nil || page.Revision != 1 || page.Values["proxyUrl"] != valid.Values["proxyUrl"] {
		t.Fatal("proxy settings not saved", err)
	}
	// 普通设置写入按修订冲突；相同 Idempotency-Key 不把旧修订转换为成功回放。
	if w := request("PUT", path, valid, true, true); w.Code != 409 {
		t.Fatal("stale settings revision accepted")
	}
	other := request("GET", "/api/v1/plugin-settings/musicfs.scraper.itunes", nil, true, false)
	if other.Code != 200 || strings.Contains(other.Body.String(), "17890") {
		t.Fatal("proxy settings crossed plugin boundary")
	}
	if w := request("POST", path+"/actions/execute", map[string]any{}, true, true); w.Code != 404 {
		t.Fatal("scraper undeclared action accepted")
	}
	private, err := settings.New(store, scrapers.SettingsProviders()...).Read(t.Context(), "other", "musicfs.scraper.bangumi")
	if err != nil || private.Revision != 0 || private.Values["proxyMode"] != "environment" {
		t.Fatal("proxy settings crossed owner boundary", err)
	}
	managed := request("GET", "/api/v1/admin/plugins", nil, true, false)
	if managed.Code != 200 {
		t.Fatal(managed.Body.String())
	}
	testkit.ValidateHTTP(t, "PluginManagementSnapshot", managed.Body.Bytes())
	var management plugincontrol.Snapshot
	if err := json.Unmarshal(managed.Body.Bytes(), &management); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, item := range management.Items {
		if strings.HasPrefix(item.PluginID, "musicfs.scraper.") && !item.SettingsAvailable {
			t.Fatal("scraper management snapshot omitted settings", item.PluginID)
		}
		if strings.HasPrefix(item.PluginID, "musicfs.scraper.") {
			found++
		}
	}
	if found != 7 {
		t.Fatalf("expected seven configurable scrapers, got %d", found)
	}
	if strings.Contains(managed.Body.String(), "17890") || strings.Contains(managed.Body.String(), "proxyUrl") {
		t.Fatal("management snapshot disclosed proxy settings")
	}
}

package api_test

import (
	"bytes"
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPluginSettingsHTTPValidationAuthorizationAndSchema(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	a := matching.NewAutomation(store, store, matching.New(store, nil, standard.Plugin{}), standard.Detector())
	server.RegisterPluginSettings(settings.New(store, a.Provider()))
	path := "/api/v1/plugin-settings/" + standard.PluginID
	call := func(method, url string, body any, csrf bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, url, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "settings-action-key")
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	w := call("GET", path, nil, true)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "PluginSettingsPage", w.Body.Bytes())
	valid := map[string]any{"expectedRevision": 0, "schemaRevision": "matching-settings-v1", "values": map[string]any{"automatic": false, "minimumScore": 35}}
	if w = call("PUT", path, valid, false); w.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	for _, values := range []any{map[string]any{"unknown": true}, map[string]any{"automatic": "true"}, map[string]any{"minimumScore": 101}, map[string]any{"minimumScore": nil}, map[string]any{"minimumScore": "35"}, nil} {
		body := map[string]any{"expectedRevision": 0, "schemaRevision": "matching-settings-v1", "values": values}
		if w = call("PUT", path, body, true); w.Code != 400 {
			t.Fatalf("invalid setting accepted: %s", w.Body.String())
		}
	}
	if w = call("PUT", path, valid, true); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "PluginSettingsPage", w.Body.Bytes())
	if w = call("PUT", path, valid, true); w.Code != 409 {
		t.Fatal("stale revision accepted")
	}
	if w = call("POST", path+"/actions/execute-script", map[string]any{}, true); w.Code != 404 {
		t.Fatal("undeclared plugin action accepted")
	}
	if w = call("POST", path+"/actions/scan", map[string]any{"url": "https://untrusted.invalid"}, true); w.Code != 400 {
		t.Fatal("untrusted action input accepted")
	}
	if w = call("POST", path+"/actions/scan", map[string]any{}, true); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "PluginSettingsJob", w.Body.Bytes())
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("anonymous plugin settings exposed")
	}
}

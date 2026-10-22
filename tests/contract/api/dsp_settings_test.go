package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestDSPSettingsHTTPScopesValidationAndRevision(t *testing.T) {
	_, store, _ := libraryHTTP(t)
	authRepo := &memoryAuth{sessions: map[string]auth.Session{}}
	server := api.New(api.Options{Auth: auth.New(authRepo, testToken, []string{origin}, time.Hour), SecureCookie: true})
	service := dspsettings.New(store, func() dspsettings.Config {
		return dspsettings.Config{Available: true, OutputLayouts: []string{"stereo", "5.1", "7.1"},
			Defaults: dspsettings.Options{Chain: []audio.NodeSpec{}, OutputLayout: "stereo", BufferFrames: 4096}}
	})
	server.RegisterDSPSettings(service)
	cookie, session := login(t, server)
	call := func(method, path string, body any, csrf bool, key string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	path := "/api/v1/dsp/settings"
	for _, endpoint := range []string{path, path + "/global"} {
		w := call("GET", endpoint, nil, true, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "DSPSettingsPage", w.Body.Bytes())
	}
	options := map[string]any{"chain": []any{map[string]any{"name": "equalizer", "version": 1, "params": map[string]any{"gainDB": -3}}}, "outputLayout": "stereo", "bufferFrames": 64}
	body := map[string]any{"expectedRevision": 0, "schemaRevision": dspsettings.SchemaRevision, "settings": options}
	raw, _ := json.Marshal(body)
	testkit.ValidateHTTP(t, "DSPSettingsRequest", raw)
	if w := call("PUT", path, body, false, "csrf-invalid-key"); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Body.String())
	}
	if w := call("PUT", path, body, true, ""); w.Code != 400 {
		t.Fatal("missing operation key accepted")
	}
	w := call("POST", path+"/validate", map[string]any{"settings": options}, true, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "DSPPreview", w.Body.Bytes())
	if w = call("PUT", path+"/global", body, true, "global-http-save"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "DSPSettingsPage", w.Body.Bytes())
	if w = call("PUT", path, body, true, "personal-http-save"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call("PUT", path, body, true, "personal-http-save"); w.Code != 200 {
		t.Fatal("same request retry failed", w.Body.String())
	}
	if w = call("PUT", path, body, true, "stale-personal-save"); w.Code != 409 {
		t.Fatal("stale request accepted")
	}
	for _, change := range []map[string]any{
		{"chain": []any{}, "outputLayout": "stereo", "bufferFrames": 63},
		{"chain": []any{}, "outputLayout": "stereo", "bufferFrames": 16385},
		{"chain": []any{}, "outputLayout": "stereo", "bufferFrames": 64.5},
		{"chain": []any{map[string]any{"name": "gain", "version": 1, "params": map[string]any{"gainDB": nil}}}, "outputLayout": "stereo", "bufferFrames": 64},
		{"chain": []any{map[string]any{"name": "channelmix", "version": 1, "params": map[string]any{"outputChannels": 6}}}, "outputLayout": "stereo", "bufferFrames": 64},
		{"chain": []any{map[string]any{"name": "run-command", "version": 1}}, "outputLayout": "stereo", "bufferFrames": 64},
		{"chain": []any{}, "outputLayout": "stereo", "bufferFrames": 64, "executable": "C:/private/tool.exe"},
	} {
		if w = call("POST", path+"/validate", map[string]any{"settings": change}, true, ""); w.Code != 400 {
			t.Fatal("invalid DSP settings accepted", w.Code, w.Body.String())
		}
	}
	inherit := map[string]any{"expectedRevision": 1, "schemaRevision": dspsettings.SchemaRevision, "inheritGlobal": true}
	if w = call("PUT", path, inherit, true, "inherit-http-save"); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var page dspsettings.Page
	json.Unmarshal(w.Body.Bytes(), &page)
	if !page.UsesGlobal || page.Settings.BufferFrames != 64 {
		t.Fatal("inheritance not applied", page)
	}
	if w = call("PUT", path+"/global", inherit, true, "global-cannot-inherit"); w.Code != 400 {
		t.Fatal("self inheritance accepted")
	}
	stored, err := authRepo.FindAuth(t.Context(), auth.Digest(cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	stored.User = auth.User{ID: "listener", Role: "user"}
	if err = authRepo.SaveAuth(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if w = call(method, path+"/global", body, true, "unauthorized-global"); w.Code != 403 {
			t.Fatal("global settings bypass", w.Code, w.Body.String())
		}
	}
	if w = call("GET", path, nil, false, ""); w.Code != 200 {
		t.Fatal("personal settings denied", w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &page)
	if page.Revision != 0 || !page.UsesGlobal {
		t.Fatal("another user's personal settings leaked", page)
	}
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 401 {
		t.Fatal("anonymous settings exposed")
	}
}

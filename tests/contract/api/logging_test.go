package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func loggingServer(t *testing.T) (*api.Server, *logging.Manager, *memoryAuth) {
	t.Helper()
	manager, err := logging.New(logging.Options{Directory: t.TempDir(), Level: "info", Console: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := &memoryAuth{sessions: map[string]auth.Session{}}
	service := auth.New(repository, testToken, []string{origin}, time.Hour)
	server := api.New(api.Options{Auth: service, SecureCookie: true, Logger: manager.Logger()})
	server.RegisterLogging(manager)
	return server, manager, repository
}

func TestLoggingReadUpdateReplacementAndAuthorization(t *testing.T) {
	server, manager, repository := loggingServer(t)
	cookie, session := login(t, server)
	request := func(method, body string, authenticated, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/admin/logging", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(cookie)
		}
		if csrf {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{"GET", "PUT"} {
		if w := request(method, `{"level":"trace","plugins":{}}`, false, false); w.Code != 401 {
			t.Fatalf("anonymous %s returned %d", method, w.Code)
		}
	}
	if w := request("PUT", `{"level":"trace","plugins":{}}`, true, false); w.Code != 403 || manager.Settings().Level != "info" {
		t.Fatalf("update without CSRF returned %d or changed levels", w.Code)
	}
	if w := request("GET", "", true, false); w.Code != 200 || strings.Contains(w.Body.String(), "directory") {
		t.Fatalf("logging read returned %d: %s", w.Code, w.Body.String())
	} else {
		testkit.ValidateHTTP(t, "LoggingSettings", w.Body.Bytes())
	}
	for _, body := range []string{
		`{"level":"debug","plugins":{"musicfs.plugin.source.localfiles":"trace","musicfs.scraper.local":"warn"}}`,
		`{"level":"info","plugins":{"musicfs.scraper.local":"error"}}`,
		`{"level":"info","plugins":{}}`,
	} {
		w := request("PUT", body, true, true)
		if w.Code != 200 {
			t.Fatalf("valid update returned %d: %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "LoggingSettings", w.Body.Bytes())
		var want logging.Settings
		if err := json.Unmarshal([]byte(body), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(manager.Settings(), want) {
			t.Fatalf("update did not replace all settings: %+v", manager.Settings())
		}
	}
	repository.mu.Lock()
	stored := repository.sessions[auth.Digest(cookie.Value)]
	stored.User.Role = "user"
	repository.sessions[stored.ID] = stored
	repository.mu.Unlock()
	for _, method := range []string{"GET", "PUT"} {
		if w := request(method, `{"level":"trace","plugins":{}}`, true, true); w.Code != 403 {
			t.Fatalf("non-admin %s returned %d", method, w.Code)
		}
	}
}

func TestLoggingInvalidUpdatesPreserveSettings(t *testing.T) {
	server, manager, _ := loggingServer(t)
	cookie, session := login(t, server)
	before := manager.Settings()
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"level":"debug"}`, `{"plugins":{}}`,
		`{"level":null,"plugins":{}}`, `{"level":"","plugins":{}}`,
		`{"level":"INFO","plugins":{}}`, `{"level":"verbose","plugins":{}}`,
		`{"level":"info","plugins":null}`, `{"level":"info","plugins":[]}`,
		`{"level":"info","plugins":{"musicfs.scraper.local":null}}`,
		`{"level":"info","plugins":{"../private":"trace"}}`,
		`{"level":"info","plugins":{"CON.log":"trace"}}`,
		`{"level":"info","plugins":{"plugin.":"trace"}}`,
		`{"level":"info","plugins":{"musicfs.scraper.local":"debug","musicfs.plugin.source.localfiles":"verbose"}}`,
		`{"level":"info","plugins":{},"directory":"private"}`,
		`{"level":"info","level":"trace","plugins":{}}`,
		`{"level":"info","plugins":{"musicfs.scraper.local":"debug","musicfs.scraper.local":"trace"}}`,
		`{"level":"info","plugins":{}} {"level":"trace","plugins":{}}`,
	} {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/logging", strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 400 || !reflect.DeepEqual(manager.Settings(), before) {
			t.Fatalf("invalid update returned %d or mutated settings: %s", w.Code, body)
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
}

func TestLoggingOpenAPIAndSettingsSchema(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	path, ok := doc["paths"].(map[string]any)["/admin/logging"].(map[string]any)
	if !ok || path["get"].(map[string]any)["operationId"] != "getLoggingSettings" || path["put"].(map[string]any)["operationId"] != "setLoggingSettings" {
		t.Fatal("logging management operations missing")
	}
	parameters := path["put"].(map[string]any)["parameters"].([]any)
	if len(parameters) != 1 || parameters[0].(map[string]any)["$ref"] != "#/components/parameters/CSRF" {
		t.Fatal("logging update CSRF contract missing")
	}
	schema := testkit.ContractSchema(t, "http", "LoggingSettings")
	for _, raw := range []string{
		`{"level":"info","plugins":{}}`,
		`{"level":"trace","plugins":{"musicfs.plugin.source.localfiles":"debug"}}`,
	} {
		if err := schema.Validate(testkit.DecodeJSON(t, []byte(raw))); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"level":"INFO","plugins":{}}`, `{"level":"info"}`,
		`{"level":"info","plugins":null}`, `{"level":"info","plugins":{"../private":"trace"}}`,
		`{"level":"info","plugins":{"CON.log":"trace"}}`, `{"level":"info","plugins":{"plugin.":"trace"}}`,
		`{"level":"info","plugins":{},"directory":"private"}`,
	} {
		if schema.Validate(testkit.DecodeJSON(t, []byte(raw))) == nil {
			t.Fatalf("invalid logging response accepted: %s", raw)
		}
	}
}

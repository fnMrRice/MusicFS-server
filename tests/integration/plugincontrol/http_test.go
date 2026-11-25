package plugincontrol_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

const httpOrigin = "http://127.0.0.1:8080"

type httpAuth struct {
	mu       sync.Mutex
	sessions map[string]auth.Session
}

func (s *httpAuth) SaveAuth(_ context.Context, v auth.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[v.ID] = v
	return nil
}
func (s *httpAuth) FindAuth(_ context.Context, id string) (auth.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.sessions[id]; ok {
		return v, nil
	}
	return auth.Session{}, domain.Fail("NOT_FOUND", "Session not found")
}
func (s *httpAuth) DeleteAuth(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}
func (s *httpAuth) UpdateAuthExpiry(_ context.Context, id string, expiry time.Time) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if !ok || !v.ExpiresAt.After(time.Now()) {
		return time.Time{}, nil
	}
	if expiry.After(v.ExpiresAt) {
		v.ExpiresAt = expiry
		s.sessions[id] = v
	}
	return v.ExpiresAt, nil
}

func managementHTTP(t *testing.T, manager *plugincontrol.Manager) (*api.Server, map[string]*http.Cookie) {
	t.Helper()
	repo := &httpAuth{sessions: map[string]auth.Session{}}
	cookies := map[string]*http.Cookie{}
	for _, role := range []string{"admin", "user", "other-admin"} {
		value := domain.NewID()
		kind := role
		if role == "other-admin" {
			kind = "admin"
		}
		err := repo.SaveAuth(context.Background(), auth.Session{ID: auth.Digest(value), User: auth.User{ID: role, Role: kind}, CSRFToken: "test-csrf", ExpiresAt: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		cookies[role] = &http.Cookie{Name: api.CookieName, Value: value}
	}
	server := api.New(api.Options{Auth: auth.New(repo, "test-admin-token", []string{httpOrigin}, time.Hour), Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	server.RegisterPluginManagement(manager)
	return server, cookies
}

func managementRequest(server http.Handler, cookie *http.Cookie, method, path, body, revision, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", httpOrigin)
	r.Header.Set("X-CSRF-Token", "test-csrf")
	r.Header.Set("If-Match", revision)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	return w
}

func requireHTTP(t *testing.T, w *httptest.ResponseRecorder, status int, failure string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	if failure != "" {
		var body struct{ Error domain.Error }
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code != failure {
			t.Fatalf("HTTP error wanted %s: %s", failure, w.Body.String())
		}
	}
}

func TestPluginManagementHTTPAuthorizationCSRFRevisionAndIdempotency(t *testing.T) {
	h := setup(t)
	server, cookies := managementHTTP(t, h.manager)
	for _, route := range []string{"/admin/plugins", "/admin/plugins/musicfs.plugin.decoder.ncm/instances", "/admin/plugin-instances/" + ncmInstanceID + "/configuration", "/admin/plugin-operations/missing"} {
		requireHTTP(t, managementRequest(server, nil, "GET", route, "", "", ""), 401, "AUTH_REQUIRED")
		requireHTTP(t, managementRequest(server, cookies["user"], "GET", route, "", "", ""), 403, "ACCESS_DENIED")
	}
	w := managementRequest(server, cookies["admin"], "GET", "/admin/plugins", "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "PluginManagementSnapshot", w.Body.Bytes())
	revision := w.Header().Get("ETag")
	if revision != `"`+h.manager.Snapshot().Revision+`"` || strings.Contains(w.Body.String(), "installed-worker") || strings.Contains(w.Body.String(), "executable") {
		t.Fatal("missing ETag or private executable leaked", w.Body.String())
	}
	state := "/admin/plugin-instances/" + ncmInstanceID + "/state"
	body := `{"enabled":false,"mode":"drain"}`
	requireHTTP(t, managementRequest(server, cookies["user"], "PUT", state, body, revision, "user-state"), 403, "ACCESS_DENIED")
	r := httptest.NewRequest("PUT", "/api/v1"+state, strings.NewReader(body))
	r.AddCookie(cookies["admin"])
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	requireHTTP(t, w, 403, "ACCESS_DENIED")
	requireHTTP(t, managementRequest(server, cookies["admin"], "PUT", state, body, "", "missing-revision"), 400, "INVALID_ARGUMENT")
	requireHTTP(t, managementRequest(server, cookies["admin"], "PUT", state, body, revision, ""), 400, "INVALID_ARGUMENT")
	requireHTTP(t, managementRequest(server, cookies["admin"], "PUT", state, body, `"`+strings.Repeat("0", 64)+`"`, "stale-revision"), 412, "PRECONDITION_FAILED")
	requireHTTP(t, managementRequest(server, cookies["admin"], "PUT", state, `{"enabled":false,"executable":"private-path"}`, revision, "extra-field"), 400, "INVALID_ARGUMENT")
	w = managementRequest(server, cookies["admin"], "PUT", state, body, revision, "disable-http")
	requireHTTP(t, w, 202, "")
	testkit.ValidateHTTP(t, "PluginOperation", w.Body.Bytes())
	var operation plugincontrol.Operation
	if json.Unmarshal(w.Body.Bytes(), &operation) != nil || operation.ID == "" {
		t.Fatal("missing operation", w.Body.String())
	}
	waitStatus(t, h, operation, "succeeded")
	if !operation.RestartRequired {
		t.Fatal("saved disable omitted restart requirement")
	}
	w = managementRequest(server, cookies["admin"], "PUT", state, body, revision, "disable-http")
	requireHTTP(t, w, 202, "")
	testkit.ValidateHTTP(t, "PluginOperation", w.Body.Bytes())
	var replay plugincontrol.Operation
	if json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.ID != operation.ID {
		t.Fatal("idempotent HTTP request created another operation")
	}
	requireHTTP(t, managementRequest(server, cookies["admin"], "PUT", state, `{"enabled":true}`, revision, "disable-http"), 409, "CONFLICT")
	requireHTTP(t, managementRequest(server, cookies["other-admin"], "GET", "/admin/plugin-operations/"+operation.ID, "", "", ""), 404, "NOT_FOUND")
	w = managementRequest(server, cookies["admin"], "GET", "/admin/plugin-operations/"+operation.ID, "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "PluginOperation", w.Body.Bytes())
	w = managementRequest(server, cookies["admin"], "GET", "/admin/plugins/musicfs.plugin.decoder.ncm/instances", "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "ManagedPluginInstances", w.Body.Bytes())
	if !strings.Contains(w.Body.String(), `"configuredEnabled":false`) || !strings.Contains(w.Body.String(), `"effectiveEnabled":true`) || !strings.Contains(w.Body.String(), `"runtimeState":"ready"`) || !strings.Contains(w.Body.String(), `"reason":"RESTART_REQUIRED"`) {
		t.Fatal("instance state did not distinguish saved disable from current runtime", w.Body.String())
	}
}

func configurationManager(t *testing.T) (*plugincontrol.Manager, string) {
	t.Helper()
	t.Setenv("MUSICFS_ADMIN_TOKEN", "test-only-long-administrator-token")
	dir := t.TempDir()
	private := filepath.Join(dir, "private-music-root")
	doc := `configVersion: 1
listen: '127.0.0.1:8080'
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
plugins:
  - pluginId: musicfs.plugin.source.localfiles
    executable: './private-provider-worker'
    enabled: false
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        name: Local
        enabled: false
        config:
          musicRoots:
            - id: music
              name: Music
              path: ` + strconv.Quote(private) + `
  - pluginId: musicfs.plugin.decoder.ffmpeg
    executable: './private-audio-worker'
    enabled: false
    instances:
      - instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77
        name: Plugin instance
        enabled: false
        config:
          ffmpegPath: './private-ffmpeg'
          ffprobePath: './private-ffprobe'
  - pluginId: musicfs.plugin.decoder.ncm
    executable: './private-ncm-worker'
    enabled: false
    instances:
      - instanceId: d1971de9-e153-57c6-ac53-82dfb631da0b
        name: Decoder
        enabled: false
        config: {}
`
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.Open(context.Background(), filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	store, err := config.OpenStore(context.Background(), path, nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := plugincontrol.New(context.Background(), store.Current(), store, repo)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start()
	t.Cleanup(manager.Close)
	return manager, private
}

func TestPluginManagementHTTPConfigurationSchemaAndWriteOnlyPaths(t *testing.T) {
	manager, private := configurationManager(t)
	server, cookies := managementHTTP(t, manager)
	w := managementRequest(server, cookies["admin"], "GET", "/admin/plugins", "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "PluginManagementSnapshot", w.Body.Bytes())
	for _, id := range []string{localInstanceID, audioInstanceID, ncmInstanceID} {
		w := managementRequest(server, cookies["admin"], "GET", "/admin/plugin-instances/"+id+"/configuration", "", "", "")
		requireHTTP(t, w, 200, "")
		testkit.ValidateHTTP(t, "ManagedPluginConfiguration", w.Body.Bytes())
		for _, forbidden := range []string{private, filepath.Base(private), "private-ffmpeg", "private-ffprobe", "private-provider-worker", "private-audio-worker", "private-ncm-worker"} {
			if strings.Contains(w.Body.String(), forbidden) {
				t.Fatalf("configuration leaked %s", forbidden)
			}
		}
		var view plugincontrol.Configuration
		if json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Schema["additionalProperties"] != false {
			t.Fatal("configuration lacks strict writable schema", w.Body.String())
		}
		if id == localInstanceID && (len(view.Roots) != 1 || !view.Roots[0].PathConfigured || view.ImportOptions == nil || view.Watch == nil) {
			t.Fatal("local configuration summary incomplete", view)
		}
		if id == audioInstanceID && (!view.FFmpegConfigured || !view.FFprobeConfigured) {
			t.Fatal("audio path presence missing", view)
		}
		if id != ncmInstanceID && !strings.Contains(w.Body.String(), `"writeOnly":true`) {
			t.Fatal("path input schema is not write-only")
		}
	}
	path := "/admin/plugin-instances/" + localInstanceID + "/configuration"
	revision := `"` + manager.Snapshot().Revision + `"`
	for index, body := range []string{`{}`, `{"role":"audio"}`, `{"executable":"private"}`, `{"ffmpegPath":"private"}`, `{"importDefaults":{"recursive":true,"maxDepth":-1}}`, `{"musicRoots":[{"id":"music","name":"Music","path":null}]}`} {
		w := managementRequest(server, cookies["admin"], "PUT", path, body, revision, "bad-settings-"+strconv.Itoa(index))
		requireHTTP(t, w, 400, "INVALID_ARGUMENT")
		if strings.Contains(w.Body.String(), "private") {
			t.Fatal("schema validation echoed a private input value")
		}
	}
	w = managementRequest(server, cookies["admin"], "PUT", path, `{"name":"Renamed local","musicRoots":[{"id":"music","name":"Renamed root"}]}`, revision, "save-http-settings")
	requireHTTP(t, w, 202, "")
	testkit.ValidateHTTP(t, "PluginOperation", w.Body.Bytes())
	w = managementRequest(server, cookies["admin"], "GET", path, "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "ManagedPluginConfiguration", w.Body.Bytes())
	var view plugincontrol.Configuration
	if json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Name != "Renamed local" || view.Roots[0].Name != "Renamed root" || !view.Roots[0].PathConfigured {
		t.Fatal("partial root update lost its preserved private path", w.Body.String())
	}
	path = "/admin/plugin-instances/" + ncmInstanceID + "/configuration"
	revision = `"` + manager.Snapshot().Revision + `"`
	w = managementRequest(server, cookies["admin"], "PUT", path, `{"name":"Renamed decoder"}`, revision, "save-http-ncm-settings")
	requireHTTP(t, w, 202, "")
	testkit.ValidateHTTP(t, "PluginOperation", w.Body.Bytes())
	w = managementRequest(server, cookies["admin"], "GET", path, "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "ManagedPluginConfiguration", w.Body.Bytes())
	if json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Name != "Renamed decoder" || view.ImportOptions != nil || view.Watch != nil || len(view.Roots) != 0 {
		t.Fatal("decoder configuration has invalid provider fields", w.Body.String())
	}
	w = managementRequest(server, cookies["admin"], "GET", "/admin/plugins", "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "PluginManagementSnapshot", w.Body.Bytes())
}

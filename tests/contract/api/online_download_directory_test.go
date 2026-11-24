package api_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineDownloadDirectoryHTTP(t *testing.T) {
	_, store, _ := libraryHTTP(t)
	repo := &memoryAuth{sessions: map[string]auth.Session{}}
	server := api.New(api.Options{Auth: auth.New(repo, testToken, []string{origin}, time.Hour), SecureCookie: true})
	cache := t.TempDir()
	deployment := config.Config{WebRoot: t.TempDir(), DataRoot: t.TempDir()}
	player, err := playback.New(t.Context(), store, nil, nil, playback.Config{CacheRoot: cache,
		ValidateDownloadDirectory: deployment.ValidateDownloadDirectory, MaxSessions: 2, MaxCacheBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	server.RegisterPlayback(player)
	cookie, session := login(t, server)
	call := func(method, body string, signedIn, csrf bool, status int) onlinecache.DirectorySettings {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/admin/online-download-directory", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if signedIn {
			r.AddCookie(cookie)
		}
		if csrf {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: got %d, want %d: %s", method, w.Code, status, w.Body.String())
		}
		var directory onlinecache.DirectorySettings
		if status == 200 {
			testkit.ValidateHTTP(t, "OnlineDownloadDirectory", w.Body.Bytes())
			if err := json.Unmarshal(w.Body.Bytes(), &directory); err != nil {
				t.Fatal(err)
			}
		} else {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return directory
	}
	initial := call("GET", "", true, false, 200)
	if initial.Path != "" || initial.EffectivePath != filepath.Join(cache, "playback", "online") {
		t.Fatalf("unexpected default directory: %+v", initial)
	}
	const reset = `{"path":""}`
	call("GET", "", false, false, 401)
	call("PUT", reset, true, false, 403)
	for _, invalid := range []string{`{}`, `{"path":null}`, `{"path":23}`, `{"path":"","other":true}`, `{"path":"relative"}`} {
		call("PUT", invalid, true, true, 400)
	}
	for _, reserved := range []string{deployment.WebRoot, filepath.Join(deployment.DataRoot, "providers")} {
		destination := filepath.Join(reserved, "downloads")
		body, err := json.Marshal(map[string]string{"path": destination})
		if err != nil {
			t.Fatal(err)
		}
		call("PUT", string(body), true, true, 400)
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("reserved directory was created: %v", err)
		}
	}
	base := t.TempDir()
	body, err := json.Marshal(map[string]string{"path": base})
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "SetOnlineDownloadDirectory", body)
	saved := call("PUT", string(body), true, true, 200)
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Path != resolvedBase || filepath.Dir(saved.EffectivePath) != resolvedBase || call("GET", "", true, false, 200) != saved {
		t.Fatalf("unexpected saved directory: %+v", saved)
	}
	if call("PUT", string(body), true, true, 200) != saved || call("PUT", reset, true, true, 200) != initial {
		t.Fatal("repeat save or reset returned a different directory")
	}
	stored, err := repo.FindAuth(t.Context(), auth.Digest(cookie.Value))
	if err != nil {
		t.Fatal(err)
	}
	stored.User.Role = "user"
	if err := repo.SaveAuth(t.Context(), stored); err != nil {
		t.Fatal(err)
	}
	call("GET", "", true, false, 403)
	call("PUT", string(body), true, true, 403)
}

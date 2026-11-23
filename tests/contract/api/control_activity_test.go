package api_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func TestControlActivityCoalescesAndScopesChanges(t *testing.T) {
	repo := &memoryAuth{sessions: map[string]auth.Session{}}
	s := api.New(api.Options{Auth: auth.New(repo, testToken, []string{origin}, time.Hour)})
	host := controlHTTP(t, s, nil)
	connect := func(user, role string) *websocket.Conn {
		t.Helper()
		token := domain.NewID()
		session := auth.Session{ID: auth.Digest(token), User: auth.User{ID: user, Role: role}, CSRFToken: domain.NewID(), ExpiresAt: time.Now().Add(time.Hour)}
		if err := repo.SaveAuth(t.Context(), session); err != nil {
			t.Fatal(err)
		}
		conn := dialControl(t, host, &http.Cookie{Name: api.CookieName, Value: token}, session.CSRFToken)
		readControl(t, conn, "ready")
		return conn
	}
	listener := connect("listener", "user")
	admin := connect("admin", "admin")
	other := connect("other", "user")

	// 各连接都收到自己的标记事件；错误广播的资源会直接出现在断言中。
	s.NotifyScan("listener", true)
	s.NotifyPluginJobs("other")

	check := func(conn *websocket.Conn, want ...string) {
		t.Helper()
		message := readControl(t, conn, "event")
		var activity struct{ Resources []string }
		if message.Event != "activity.changed" || json.Unmarshal(message.Data, &activity) != nil || !slices.Equal(activity.Resources, want) {
			t.Fatalf("unexpected activity: %s %s, want %v", message.Event, message.Data, want)
		}
	}
	check(listener, "scans", "tasks")
	check(admin, "tasks")
	check(other, "pluginJobs")
	s.NotifyPlaylistSync("listener")
	check(listener, "playlistSync")
}

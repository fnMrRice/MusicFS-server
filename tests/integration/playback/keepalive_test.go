package playback_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

func TestHTTPKeepAliveRenewsLoginAndBufferedPlayback(t *testing.T) {
	// SQLite 用真实时钟检查登录期限。从当前时间开始，再推进客户端和宿主的虚拟时钟。
	start := time.Now().UTC().Truncate(time.Second)
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Until(start))
		player, repo, _, request, _ := playbackService(t)
		const origin = "http://localhost"
		login := auth.New(repo, "test-credential", []string{origin}, 20*time.Minute)
		who, cookie, err := login.Login(t.Context(), "test-credential", origin, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		server := api.New(api.Options{Auth: login})
		server.RegisterPlayback(player)
		d, err := player.Create(t.Context(), who, request, "original-playback")
		if err != nil {
			t.Fatal(err)
		}
		event := personal.PlaybackStarted{SourceTrackID: d.SourceTrackID, PlaybackSessionID: d.SessionID, AttemptID: d.AttemptID}
		first, err := repo.RecordPlaybackStarted(t.Context(), who, event)
		if err != nil {
			t.Fatal(err)
		}
		ping := func(csrf string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/playback/sessions/"+d.SessionID+"/keepalive", nil)
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", csrf)
			r.AddCookie(&http.Cookie{Name: api.CookieName, Value: cookie})
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			return w
		}
		if w := ping(""); w.Code != http.StatusForbidden {
			t.Fatalf("keepalive without CSRF returned %d", w.Code)
		}
		if _, err := player.KeepAlive(t.Context(), auth.Session{ID: "other-session"}, d.SessionID); err == nil {
			t.Fatal("another login renewed playback")
		}
		// 模拟单曲连续播放一小时，期间只有保活请求。
		for range 6 {
			time.Sleep(10 * time.Minute)
			w := ping(who.CSRFToken)
			var response struct{ ExpiresAt time.Time }
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || time.Until(response.ExpiresAt) != 20*time.Minute {
				t.Fatalf("keepalive returned %d %s", w.Code, w.Body.String())
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Value != cookie || !cookies[0].Expires.Equal(response.ExpiresAt) {
				t.Fatal("session cookie was not renewed alongside playback")
			}
		}
		stream, err := player.OpenStream(t.Context(), who, d.SessionID, d.AttemptID, false)
		if err != nil {
			t.Fatal("original attempt no longer playable", err)
		}
		stream.Close()
		repeat, err := repo.RecordPlaybackStarted(t.Context(), who, event)
		if err != nil || !first.RecordedAt.Equal(repeat.RecordedAt) {
			t.Fatal("extended playback was recorded twice", err)
		}
		time.Sleep(21 * time.Minute)
		synctest.Wait()
		if w := ping(who.CSRFToken); w.Code != http.StatusUnauthorized {
			t.Fatalf("idle login returned %d", w.Code)
		}
		if _, err := player.KeepAlive(t.Context(), who, d.SessionID); err == nil {
			t.Fatal("expired playback was renewed")
		}
	})
}

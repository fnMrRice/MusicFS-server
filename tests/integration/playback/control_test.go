package playback_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPlaybackWebSocketControlsKeepHTTPAudioAndRange(t *testing.T) {
	player, repo, _, create, path := playbackService(t)
	authentication := auth.New(repo.Store, "test-control-secret", []string{"http://localhost"}, time.Hour)
	who, cookie, err := authentication.Login(t.Context(), "test-control-secret", "http://localhost", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	server := api.New(api.Options{Auth: authentication, InstanceID: "test-instance", Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	server.RegisterPlayback(player)
	host := httptest.NewServer(server)
	defer func() { server.CloseControl(); server.WaitControl(); host.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cookieHeader := (&http.Cookie{Name: api.CookieName, Value: cookie}).String()
	conn, _, err := websocket.Dial(ctx, host.URL+"/api/v1/control", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://localhost"}, "Cookie": []string{cookieHeader}}, Subprotocols: []string{"musicfs.control.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "auth", "csrfToken": who.CSRFToken}); err != nil {
		t.Fatal(err)
	}
	var ready struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &ready); err != nil || ready.Type != "ready" {
		t.Fatal("missing ready", err)
	}
	call := func(id, method, route string, body any, key string, status int) json.RawMessage {
		t.Helper()
		message := map[string]any{"type": "request", "id": id, "method": method, "path": route}
		if body != nil {
			message["body"] = body
		}
		if key != "" {
			message["idempotencyKey"] = key
		}
		if err := wsjson.Write(ctx, conn, message); err != nil {
			t.Fatal(err)
		}
		var result struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		}
		if err := wsjson.Read(ctx, conn, &result); err != nil {
			t.Fatal(err)
		}
		if result.Type != "response" || result.ID != id || result.Status != status {
			t.Fatalf("control response: %+v %s", result, result.Body)
		}
		return result.Body
	}
	created := call("create-playback", "POST", "/playback/sessions", create, "create-control-key", 201)
	testkit.ValidateHTTP(t, "Playback", created)
	var description playback.Description
	if err := json.Unmarshal(created, &description); err != nil {
		t.Fatal(err)
	}
	replay := call("replay-playback", "POST", "/playback/sessions", create, "create-control-key", 201)
	if !bytes.Equal(created, replay) {
		t.Fatal("idempotent creation changed description")
	}
	wave, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	readAudio := func(url string, status int) {
		t.Helper()
		request, _ := http.NewRequestWithContext(ctx, "GET", host.URL+url, nil)
		request.Header.Set("Cookie", cookieHeader)
		request.Header.Set("Range", "bytes=0-43")
		response, err := host.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != status {
			t.Fatal("audio response", response.StatusCode, err, string(data))
		}
		if status == 206 && (!bytes.Equal(data, wave[:44]) || response.Header.Get("Content-Range") == "") {
			t.Fatal("HTTP Range audio changed")
		}
	}
	readAudio(description.StreamURL, 206)
	seek := map[string]any{"attemptId": description.AttemptID, "positionMs": 2000}
	call("seek-original", "POST", "/playback/sessions/"+description.SessionID+"/seek", seek, "seek-original-key", 409)
	call("stop-original", "DELETE", "/playback/sessions/"+description.SessionID, nil, "", 204)
	readAudio(description.StreamURL, 404)
	create.Preferences.ForceProcessing = true
	processed := call("create-processed", "POST", "/playback/sessions", create, "create-processed-key", 201)
	if err := json.Unmarshal(processed, &description); err != nil {
		t.Fatal(err)
	}
	seek["attemptId"] = description.AttemptID
	updated := call("seek-playback", "POST", "/playback/sessions/"+description.SessionID+"/seek", seek, "seek-control-key", 200)
	var next playback.Description
	if err := json.Unmarshal(updated, &next); err != nil || next.AttemptID == description.AttemptID {
		t.Fatal("seek did not replace attempt", err)
	}
	readAudio(description.StreamURL, 409)
	recovery := map[string]any{"attemptId": next.AttemptID, "positionMs": 2000, "observedError": map[string]string{"code": "MEDIA_ERROR", "message": "Synthetic media failure"}}
	recovered := call("recover-playback", "POST", "/playback/sessions/"+description.SessionID+"/recover", recovery, "recover-control-key", 200)
	if err := json.Unmarshal(recovered, &next); err != nil {
		t.Fatal(err)
	}
	call("stop-playback", "DELETE", "/playback/sessions/"+description.SessionID, nil, "", 204)
	readAudio(next.StreamURL, 404)
}

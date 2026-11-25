package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
)

type socketMessage struct {
	Type                string          `json:"type"`
	ID                  string          `json:"id"`
	Status              int             `json:"status"`
	Event               string          `json:"event"`
	InstanceID          string          `json:"instanceId"`
	HeartbeatIntervalMs int             `json:"heartbeatIntervalMs"`
	HeartbeatTimeoutMs  int             `json:"heartbeatTimeoutMs"`
	Body                json.RawMessage `json:"body"`
	Data                json.RawMessage `json:"data"`
}

func controlHTTP(t *testing.T, server *api.Server, wrapper func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	var handler http.Handler = server
	if wrapper != nil {
		handler = wrapper(handler)
	}
	host := httptest.NewServer(handler)
	t.Cleanup(func() { server.CloseControl(); server.WaitControl(); host.Close() })
	return host
}

func dialControl(t *testing.T, host *httptest.Server, cookie *http.Cookie, csrf string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	header := http.Header{"Origin": []string{origin}, "Cookie": []string{cookie.String()}}
	conn, _, err := websocket.Dial(ctx, host.URL+"/api/v1/control", &websocket.DialOptions{HTTPHeader: header, Subprotocols: []string{"musicfs.control.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	writeControl(t, conn, map[string]any{"type": "auth", "csrfToken": csrf})
	return conn
}

func writeControl(t *testing.T, conn *websocket.Conn, value any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, value); err != nil {
		t.Fatal(err)
	}
}

func readControl(t *testing.T, conn *websocket.Conn, kind string) socketMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		var message socketMessage
		if err := wsjson.Read(ctx, conn, &message); err != nil {
			t.Fatal(err)
		}
		if message.Type == kind {
			return message
		}
	}
}

func controlCall(t *testing.T, conn *websocket.Conn, id, method, path string, body any, key string) socketMessage {
	t.Helper()
	message := map[string]any{"type": "request", "id": id, "method": method, "path": path}
	if body != nil {
		message["body"] = body
	}
	if key != "" {
		message["idempotencyKey"] = key
	}
	writeControl(t, conn, message)
	result := readControl(t, conn, "response")
	if result.ID != id {
		t.Fatalf("response ID %q, want %q", result.ID, id)
	}
	return result
}

func TestControlHandshakeAndStrictRouting(t *testing.T) {
	s, _ := lifecycleAPI(t)
	s.RegisterPlayback(nil)
	cookie, session := login(t, s)
	host := controlHTTP(t, s, nil)
	for _, test := range []struct {
		name, origin, cookie, protocol, suffix string
		status                                 int
	}{
		{"anonymous", origin, "", "musicfs.control.v1", "", 401},
		{"origin", "https://invalid.test", cookie.String(), "musicfs.control.v1", "", 403},
		{"missing-origin", "", cookie.String(), "musicfs.control.v1", "", 403},
		{"protocol", origin, cookie.String(), "another.protocol", "", 400},
		{"query", origin, cookie.String(), "musicfs.control.v1", "?token=secret", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn, response, err := websocket.Dial(t.Context(), host.URL+"/api/v1/control"+test.suffix,
				&websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{test.origin}, "Cookie": []string{test.cookie}}, Subprotocols: []string{test.protocol}})
			if conn != nil {
				conn.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != test.status {
				t.Fatalf("handshake status: %v %v", response, err)
			}
		})
	}
	conn := dialControl(t, host, cookie, session.CSRFToken)
	ready := readControl(t, conn, "ready")
	if ready.InstanceID != "test-instance" || ready.HeartbeatIntervalMs != 10000 || ready.HeartbeatTimeoutMs != 30000 {
		t.Fatal("invalid ready", ready)
	}
	writeControl(t, conn, map[string]string{"type": "ping"})
	readControl(t, conn, "pong")
	for _, path := range []string{"/auth/session", "/media/uploads", "/playback/sessions/id/attempts/attempt/stream", "/lifecycle?x=1", "/lifecycle#fragment", "/lifecycle/../auth/session", "/lifecycle%2f", "//lifecycle"} {
		response := controlCall(t, conn, "route-check", "GET", path, nil, "")
		if response.Status != 400 {
			t.Fatal("non-control route accepted", path, response.Status)
		}
	}
	response := controlCall(t, conn, "bad-body-check", "POST", "/playback/sessions", map[string]string{"sourceTrackId": "track"}, "create-invalid")
	if response.Status != 400 || !strings.Contains(string(response.Body), "INVALID_ARGUMENT") {
		t.Fatal("HTTP validation was bypassed", response)
	}
	old := httptest.NewRecorder()
	s.ServeHTTP(old, httptest.NewRequest("GET", "/api/v1/events", nil))
	if old.Code != 404 {
		t.Fatal("SSE endpoint remains active")
	}
}

func TestControlLifecycleOwnershipReplayAndDisconnect(t *testing.T) {
	s, manager := lifecycleAPI(t)
	cookie, session := login(t, s)
	otherCookie, otherSession := login(t, s)
	host := controlHTTP(t, s, nil)
	conn := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, conn, "ready")
	initial := readControl(t, conn, "event")
	if initial.Event != "lifecycle.changed" {
		t.Fatal("missing lifecycle snapshot")
	}
	created := controlCall(t, conn, "create-lease", "POST", "/lifecycle/leases", map[string]string{"instanceId": "test-instance", "clientId": "client-first"}, "lease-create-key")
	var lease lifecycle.Lease
	if created.Status != 201 || json.Unmarshal(created.Body, &lease) != nil {
		t.Fatal("lease creation failed", created)
	}
	renewPath := "/lifecycle/leases/" + lease.ID + "/renew"
	renewBody := map[string]any{"instanceId": "test-instance", "sequence": 1}
	renewed := controlCall(t, conn, "renew-lease-one", "POST", renewPath, renewBody, "")
	replayed := controlCall(t, conn, "renew-lease-two", "POST", renewPath, renewBody, "")
	if renewed.Status != 200 || string(renewed.Body) != string(replayed.Body) {
		t.Fatal("renew replay changed lease expiry")
	}
	other := dialControl(t, host, otherCookie, otherSession.CSRFToken)
	readControl(t, other, "ready")
	if response := controlCall(t, other, "other-session", "POST", renewPath, renewBody, ""); response.Status != 404 {
		t.Fatal("lease ownership bypassed")
	}
	conn.CloseNow()
	if manager.Status().ActiveLeases != 1 {
		t.Fatal("disconnect released the lease")
	}
	reconnect := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, reconnect, "ready")
	released := controlCall(t, reconnect, "release-lease", "POST", "/lifecycle/leases/"+lease.ID+"/release", map[string]string{"instanceId": "test-instance"}, "")
	if released.Status != 200 || manager.Status().State != "draining" {
		t.Fatal("explicit release did not drain", released)
	}
}

func TestControlRemoteLifecycleDeniedAndRevocation(t *testing.T) {
	s, _ := lifecycleAPI(t)
	cookie, session := login(t, s)
	host := controlHTTP(t, s, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r.RemoteAddr = "192.0.2.4:2000"; next.ServeHTTP(w, r) })
	})
	conn := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, conn, "ready")
	if response := controlCall(t, conn, "remote-status", "GET", "/lifecycle", nil, ""); response.Status != 403 {
		t.Fatal("remote lifecycle exposed", response)
	}
	r := httptest.NewRequest("DELETE", "/api/v1/auth/session", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("logout failed")
	}
	writeControl(t, conn, map[string]string{"type": "ping"})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatal("revoked socket remained authorized", err)
	}
}

func TestControlRejectsInvalidFirstMessageAndReadLimit(t *testing.T) {
	for _, input := range []any{map[string]string{"type": "auth", "csrfToken": "wrong"}, map[string]string{"type": "ping"}, map[string]string{"type": "auth", "csrfToken": strings.Repeat("x", 65536)}} {
		s, _ := lifecycleAPI(t)
		cookie, _ := login(t, s)
		host := controlHTTP(t, s, nil)
		conn, _, err := websocket.Dial(t.Context(), host.URL+"/api/v1/control", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}, "Cookie": []string{cookie.String()}}, Subprotocols: []string{"musicfs.control.v1"}})
		if err != nil {
			t.Fatal(err)
		}
		_ = wsjson.Write(t.Context(), conn, input)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		_, _, err = conn.Read(ctx)
		cancel()
		conn.CloseNow()
		if err == nil {
			t.Fatal("invalid first message accepted")
		}
	}
}

func TestControlHeartbeatRunsDuringCommandAndShutdownCancels(t *testing.T) {
	s, _ := lifecycleAPI(t)
	cookie, session := login(t, s)
	entered, stopped := make(chan struct{}), make(chan struct{})
	s.Handle("POST /api/v1/playback/sessions", true, true, func(_ http.ResponseWriter, r *http.Request, _ auth.Session) error {
		close(entered)
		<-r.Context().Done()
		close(stopped)
		return r.Context().Err()
	})
	host := controlHTTP(t, s, nil)
	conn := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, conn, "ready")
	created := controlCall(t, conn, "keepalive-acquire", "POST", "/lifecycle/leases", map[string]string{"instanceId": "test-instance", "clientId": "client-waiting"}, "slow-playback-lease")
	var lease lifecycle.Lease
	if created.Status != 201 || json.Unmarshal(created.Body, &lease) != nil {
		t.Fatal("lease acquisition failed", created)
	}
	writeControl(t, conn, map[string]any{"type": "request", "id": "blocked-command", "method": "POST", "path": "/playback/sessions", "body": map[string]any{}})
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("command never entered")
	}
	writeControl(t, conn, map[string]string{"type": "ping"})
	readControl(t, conn, "pong")
	renewed := controlCall(t, conn, "keepalive-renew", "POST", "/lifecycle/leases/"+lease.ID+"/renew", map[string]any{"instanceId": "test-instance", "sequence": 1}, "")
	var current lifecycle.Lease
	if renewed.Status != 200 || json.Unmarshal(renewed.Body, &current) != nil || current.Sequence != 1 || current.ExpiresAt.Before(lease.ExpiresAt) {
		t.Fatal("playback command blocked lifecycle renewal", renewed)
	}
	s.CloseControl()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel command")
	}
}

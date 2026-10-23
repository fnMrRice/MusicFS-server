package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
)

func TestControlQueueTimeoutDoesNotExecuteExpiredCommand(t *testing.T) {
	a := auth.New(&memoryAuth{sessions: map[string]auth.Session{}}, testToken, []string{origin}, time.Hour)
	s := api.New(api.Options{Auth: a, InstanceID: "test-instance", SecureCookie: true, ControlTimeout: 50 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	cookie, session := login(t, s)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	s.Handle("POST /api/v1/playback/sessions", true, true, func(_ http.ResponseWriter, r *http.Request, _ auth.Session) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return r.Context().Err()
	})
	host := controlHTTP(t, s, nil)
	t.Cleanup(finish)
	conn := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, conn, "ready")
	writeControl(t, conn, map[string]string{"type": "request", "id": "blocking-command", "method": "POST", "path": "/playback/sessions"})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first command did not start")
	}
	writeControl(t, conn, map[string]string{"type": "request", "id": "expired-command", "method": "POST", "path": "/playback/sessions"})
	// 首条命令模拟退出较慢的业务；后续消息的总预算必须包含排队时间。
	time.Sleep(100 * time.Millisecond)
	finish()
	first, second := readControl(t, conn, "response"), readControl(t, conn, "response")
	if first.Status != 503 || second.Status != 503 || second.ID != "expired-command" || calls.Load() != 1 {
		t.Fatal("expired queued command executed", first, second, calls.Load())
	}
}

func TestControlConnectionLimitAndDuplicatePendingID(t *testing.T) {
	s, _ := lifecycleAPI(t)
	cookie, session := login(t, s)
	entered := make(chan struct{})
	s.Handle("POST /api/v1/playback/sessions", true, true, func(_ http.ResponseWriter, r *http.Request, _ auth.Session) error {
		close(entered)
		<-r.Context().Done()
		return r.Context().Err()
	})
	host := controlHTTP(t, s, nil)
	var connections []*websocket.Conn
	for range 8 {
		conn := dialControl(t, host, cookie, session.CSRFToken)
		readControl(t, conn, "ready")
		connections = append(connections, conn)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, host.URL+"/api/v1/control", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{origin}, "Cookie": []string{cookie.String()}}, Subprotocols: []string{"musicfs.control.v1"},
	})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != 429 {
		t.Fatal("ninth connection accepted", response, err)
	}
	message := map[string]string{"type": "request", "id": "duplicate-pending", "method": "POST", "path": "/playback/sessions"}
	writeControl(t, connections[0], message)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("command did not start")
	}
	writeControl(t, connections[0], message)
	for {
		var received socketMessage
		err := wsjson.Read(ctx, connections[0], &received)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatal("duplicate ID did not close connection", err)
			}
			break
		}
	}
}

func TestControlRequiresAuthFrameBeforeDeadline(t *testing.T) {
	s, _ := lifecycleAPI(t)
	cookie, _ := login(t, s)
	host := controlHTTP(t, s, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, host.URL+"/api/v1/control", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}, "Cookie": []string{cookie.String()}}, Subprotocols: []string{"musicfs.control.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	_, _, err = conn.Read(ctx)
	if err == nil || ctx.Err() != nil {
		t.Fatal("unauthenticated socket outlived auth deadline", err)
	}
}

func TestControlServerEventsDoNotExtendInboundHeartbeatDeadline(t *testing.T) {
	s, manager := lifecycleAPI(t)
	cookie, session := login(t, s)
	host := controlHTTP(t, s, nil)
	conn := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, conn, "ready")
	ctx, cancel := context.WithTimeout(t.Context(), 33*time.Second)
	defer cancel()
	// 生命周期会在启动租约超时后变化，出站快照不能延长入站心跳期限。
	for {
		var message socketMessage
		err := wsjson.Read(ctx, conn, &message)
		if err != nil {
			if ctx.Err() != nil {
				t.Fatal("server events extended the heartbeat deadline", err)
			}
			break
		}
	}
	if manager.Status().ActiveLeases != 0 {
		t.Fatal("heartbeat created a lifecycle lease")
	}
}

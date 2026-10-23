package plugincontrol_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func openControlEvents(t *testing.T, endpoint string, cookie *http.Cookie) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, endpoint+"/api/v1/control", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{httpOrigin}, "Cookie": []string{cookie.String()}}, Subprotocols: []string{"musicfs.control.v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "auth", "csrfToken": "test-csrf"}); err != nil {
		t.Fatal(err)
	}
	var ready struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &ready); err != nil || ready.Type != "ready" {
		t.Fatal("control handshake failed", err)
	}
	return conn
}

func readCapabilityEvent(t *testing.T, conn *websocket.Conn) plugincontrol.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var message struct {
		Type  string          `json:"type"`
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}
	if err := wsjson.Read(ctx, conn, &message); err != nil || message.Type != "event" || message.Event != "capabilities.changed" {
		t.Fatal("missing capability event", err, message)
	}
	var event plugincontrol.Event
	var fields map[string]any
	testkit.ValidateHTTP(t, "CapabilitiesChangedEvent", message.Data)
	if json.Unmarshal(message.Data, &event) != nil || json.Unmarshal(message.Data, &fields) != nil || len(fields) != 2 || event.Revision == "" {
		t.Fatal("event must expose only revision and sequence", string(message.Data))
	}
	return event
}

func TestPluginCapabilityWebSocketInitialUpdateAndReconnect(t *testing.T) {
	h := setup(t)
	server, cookies := managementHTTP(t, h.manager)
	host := httptest.NewServer(server)
	defer func() { server.CloseControl(); server.WaitControl(); host.Close() }()
	conn := openControlEvents(t, host.URL, cookies["user"])
	initial := readCapabilityEvent(t, conn)
	if initial.Revision != h.manager.Snapshot().Revision {
		t.Fatal("initial snapshot revision is stale")
	}
	op := change(t, h, "control-state-change", false, "drain")
	updated := readCapabilityEvent(t, conn)
	for updated.Revision != op.Revision {
		if updated.Sequence <= initial.Sequence {
			t.Fatal("capability event sequence did not advance", initial, updated)
		}
		initial = updated
		updated = readCapabilityEvent(t, conn)
	}
	if updated.Sequence <= initial.Sequence || updated.Revision == initial.Revision {
		t.Fatal("configuration change was not emitted", initial, updated)
	}
	waitStatus(t, h, op, "succeeded")
	conn.CloseNow()
	reconnected := openControlEvents(t, host.URL, cookies["user"])
	latest := readCapabilityEvent(t, reconnected)
	if latest.Revision != h.manager.Snapshot().Revision || latest.Sequence < updated.Sequence {
		t.Fatal("reconnect did not receive current capability state", latest)
	}
}

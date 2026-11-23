package api_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func controlSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	for _, family := range []string{"http", "control"} {
		uri := "https://musicfs.invalid/contracts/" + family + ".schema.json"
		if err := compiler.AddResource(uri, testkit.ReadJSON(t, "docs/contracts/"+family+".schema.json")); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile("https://musicfs.invalid/contracts/control.schema.json#/$defs/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestControlChannelContract(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	paths := doc["paths"].(map[string]any)
	if paths["/control"] == nil || paths["/events"] != nil {
		t.Fatal("WebSocket endpoint must replace the SSE contract")
	}
	samples := []struct {
		name, schema, raw string
		valid             bool
	}{
		{"auth", "ClientMessage", `{"type":"auth","csrfToken":"0123456789abcdef"}`, true},
		{"no-auth-secret", "ClientMessage", `{"type":"auth"}`, false},
		{"heartbeat", "ClientMessage", `{"type":"ping"}`, true},
		{"heartbeat-does-not-renew", "ClientMessage", `{"type":"ping","leaseId":"lease-one"}`, false},
		{"ready", "ServerMessage", `{"type":"ready","instanceId":"instance-one","heartbeatIntervalMs":10000,"heartbeatTimeoutMs":30000}`, true},
		{"pong", "ServerMessage", `{"type":"pong"}`, true},
		{"status", "Request", `{"type":"request","id":"request-one","method":"GET","path":"/lifecycle"}`, true},
		{"renew", "Request", `{"type":"request","id":"request-one","method":"POST","path":"/lifecycle/leases/lease-one/renew","body":{"instanceId":"instance-one","sequence":1}}`, true},
		{"zero-renew", "Request", `{"type":"request","id":"request-one","method":"POST","path":"/lifecycle/leases/lease-one/renew","body":{"instanceId":"instance-one","sequence":0}}`, false},
		{"seek", "Request", `{"type":"request","id":"request-one","method":"POST","path":"/playback/sessions/session-one/seek","idempotencyKey":"operation-one","body":{"attemptId":"attempt-one","positionMs":1000}}`, true},
		{"no-seek-key", "Request", `{"type":"request","id":"request-one","method":"POST","path":"/playback/sessions/session-one/seek","body":{"attemptId":"attempt-one","positionMs":1000}}`, false},
		{"stop", "Request", `{"type":"request","id":"request-one","method":"DELETE","path":"/playback/sessions/session-one"}`, true},
		{"audio-excluded", "Request", `{"type":"request","id":"request-one","method":"GET","path":"/playback/sessions/session-one/attempts/attempt-one/stream"}`, false},
		{"auth-excluded", "Request", `{"type":"request","id":"request-one","method":"DELETE","path":"/auth/session"}`, false},
		{"query-excluded", "Request", `{"type":"request","id":"request-one","method":"GET","path":"/lifecycle?sessionId=other"}`, false},
		{"stop-response", "Response", `{"type":"response","id":"request-one","status":204}`, true},
		{"error", "Response", `{"type":"response","id":"request-one","status":409,"body":{"error":{"code":"CONFLICT","message":"Conflict","requestId":"request-one","retryAfter":null}}}`, true},
		{"invalid-error", "Response", `{"type":"response","id":"request-one","status":409}`, false},
		{"capability-event", "Event", `{"type":"event","event":"capabilities.changed","data":{"revision":"revision-one","sequence":0}}`, true},
		{"activity-event", "Event", `{"type":"event","event":"activity.changed","data":{"resources":["scans","tasks"]}}`, true},
		{"unknown-resource", "Event", `{"type":"event","event":"activity.changed","data":{"resources":["userData"]}}`, false},
		{"empty-activity", "Event", `{"type":"event","event":"activity.changed","data":{"resources":[]}}`, false},
		{"unknown-event", "Event", `{"type":"event","event":"playback.position","data":{}}`, false},
	}
	for _, sample := range samples {
		t.Run(sample.name, func(t *testing.T) {
			err := controlSchema(t, sample.schema).Validate(testkit.DecodeJSON(t, []byte(sample.raw)))
			if (err == nil) != sample.valid {
				t.Fatalf("valid=%v, schema returned %v", sample.valid, err)
			}
		})
	}
}

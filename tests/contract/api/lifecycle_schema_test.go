package api_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func validateLifecycle(t *testing.T, name string, raw []byte) {
	t.Helper()
	schema := testkit.ContractSchema(t, "http", "Lifecycle"+name)
	if err := schema.Validate(testkit.DecodeJSON(t, raw)); err != nil {
		t.Fatal("lifecycle schema mismatch", err)
	}
}

func TestLifecycleOpenAPIAndInvalidRequests(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	paths := doc["paths"].(map[string]any)
	for path, operation := range map[string]string{"/lifecycle": "getLifecycle", "/lifecycle/leases": "acquireLifecycleLease", "/lifecycle/leases/{id}/renew": "renewLifecycleLease", "/lifecycle/leases/{id}/release": "releaseLifecycleLease"} {
		method := "post"
		if path == "/lifecycle" {
			method = "get"
		}
		value, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatal("lifecycle path missing", path)
		}
		if value[method].(map[string]any)["operationId"] != operation {
			t.Fatal("lifecycle operation missing", path)
		}
	}
	for name, raw := range map[string]string{"AcquireRequest": `{"instanceId":"instance-one","clientId":null}`, "RenewRequest": `{"instanceId":"instance-one","sequence":0}`, "ReleaseRequest": `{"instanceId":"instance-one","sessionId":"other-session"}`} {
		if testkit.ContractSchema(t, "http", "Lifecycle"+name).Validate(testkit.DecodeJSON(t, []byte(raw))) == nil {
			t.Fatal("invalid lifecycle request accepted", name)
		}
	}
}
func TestLifecycleSchemaMatchesActualModels(t *testing.T) {
	m, err := lifecycle.New(lifecycle.Options{Mode: lifecycle.ClientManaged, InstanceID: "instance-one"})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"starting", "ready", "draining", "stopped"} {
		switch state {
		case "ready":
			err = m.Activate()
		case "draining":
			m.BeginDrain("shutdown")
		case "stopped":
			m.Stopped()
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(m.Status())
		validateLifecycle(t, "Status", raw)
	}
	m, err = lifecycle.New(lifecycle.Options{Mode: lifecycle.ClientManaged, InstanceID: "instance-two"})
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Activate()
	p := lifecycle.Principal{UserID: "admin", SessionID: "session-one", ExpiresAt: time.Now().Add(time.Hour)}
	r := lifecycle.AcquireRequest{InstanceID: "instance-two", ClientID: "client-one"}
	lease, err := m.Acquire(p, r, "operation-one")
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"AcquireRequest": r, "RenewRequest": lifecycle.RenewRequest{InstanceID: r.InstanceID, Sequence: 1}, "ReleaseRequest": lifecycle.ReleaseRequest{InstanceID: r.InstanceID}, "Lease": lease} {
		raw, _ := json.Marshal(value)
		validateLifecycle(t, name, raw)
	}
}

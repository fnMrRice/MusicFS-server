package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func managedOnlineFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "online-management")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", path, "./tests/testkit/online-worker")
	cmd.Dir = filepath.Join("..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("online test worker build failed: %v %s", err, output)
	}
	return path
}

func managedRecommendations(t *testing.T, c *client) recommendations.Response {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/recommendations?instanceId=a0462a4b-8754-5747-a8cf-29cf4d0a874f&instanceId=18caca33-4b0c-57b7-8248-2cf8781c2448&limit=1", nil, "")
	var response recommendations.Response
	if code != 200 || json.Unmarshal(raw, &response) != nil || len(response.Sources) != 2 {
		t.Fatalf("recommendations unavailable: %d %s", code, raw)
	}
	if response.Sources[0].InstanceID != "a0462a4b-8754-5747-a8cf-29cf4d0a874f" || response.Sources[1].InstanceID != "18caca33-4b0c-57b7-8248-2cf8781c2448" {
		t.Fatal("recommendations did not preserve the requested instance order", response)
	}
	return response
}

// Lifecycle checks use the fixture's explicitly authenticated account and
// revalidate persisted credentials when a worker process is replaced.
func authenticateManagedOnline(t *testing.T, c *client, ids ...string) {
	t.Helper()
	request := func(id, action, loginID string) online.AccountView {
		t.Helper()
		method, payload := "GET", any(nil)
		if action != "state" {
			method = "POST"
			payload = map[string]string{"action": action, "loginId": loginID}
		}
		code, raw, _ := c.request(t, method, "/api/v1/online-instances/"+id+"/account", payload, domain.NewID())
		var view online.AccountView
		if code != 200 || json.Unmarshal(raw, &view) != nil {
			t.Fatalf("fixture account %s failed: %d %s", action, code, raw)
		}
		return view
	}
	pending := map[string]string{}
	for _, id := range ids {
		view := request(id, "state", "")
		switch view.State {
		case "signedIn":
		case "unverified":
			if view = request(id, "check", ""); view.State != "signedIn" {
				t.Fatal("fixture account was not verified", view)
			}
		default:
			view = request(id, "begin", "")
			pending[id] = view.LoginID
		}
	}
	if len(pending) > 0 {
		time.Sleep(3100 * time.Millisecond)
		for id, loginID := range pending {
			if view := request(id, "poll", loginID); view.State != "signedIn" {
				t.Fatal("fixture account was not authenticated", view)
			}
		}
	}
	time.Sleep(1050 * time.Millisecond)
}

func managedOnlineInstance(t *testing.T, c *client, id string) plugincontrol.Instance {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	var snapshot plugincontrol.Snapshot
	if code != 200 || json.Unmarshal(raw, &snapshot) != nil {
		t.Fatalf("plugin snapshot unavailable: %d %s", code, raw)
	}
	for _, plugin := range snapshot.Items {
		for _, instance := range plugin.Instances {
			if instance.InstanceID == id {
				return instance
			}
		}
	}
	t.Fatal("online instance missing", id)
	return plugincontrol.Instance{}
}

func TestManagedOnlineWorkerConfigurationAndRestartRequiredDisable(t *testing.T) {
	provider := managedOnlineFixture(t)
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.netease", Execution: "process", Executable: provider, Enabled: true, Instances: []config.Instance{
		{InstanceID: "a0462a4b-8754-5747-a8cf-29cf4d0a874f", Name: "First", Enabled: true},
		{InstanceID: "18caca33-4b0c-57b7-8248-2cf8781c2448", Name: "Second", Enabled: true},
	}}}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer func() { stop() }()
	c := login(t, base, cfg.AdminToken)
	authenticateManagedOnline(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f", "18caca33-4b0c-57b7-8248-2cf8781c2448")
	before := managedRecommendations(t, c)
	for _, source := range before.Sources {
		if source.Error != nil || source.Name != "Fixture listener" || len(source.Items) != 1 || source.LastSuccessAt == nil {
			t.Fatal("initial online worker was not ready", source)
		}
	}
	operation := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	pending := managedRecommendations(t, c)
	if pending.Sources[0].Error != nil || len(pending.Sources[0].Items) != 1 || !pending.Sources[0].LastSuccessAt.Equal(*before.Sources[0].LastSuccessAt) || !pending.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("pending disable changed recommendation caches", pending)
	}
	state := managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f")
	if state.RuntimeState != "ready" || !state.EffectiveEnabled || !state.AcceptingNewWork || state.Reason == nil || *state.Reason != "RESTART_REQUIRED" {
		t.Fatal("pending disable changed the online runtime", state)
	}
	time.Sleep(1050 * time.Millisecond)
	code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/search?q=Fixture", nil, "")
	if code != 200 {
		t.Fatalf("pending disable rejected a new online query: %d %s", code, raw)
	}
	generation := state.Generation
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	after := managedRecommendations(t, c)
	if after.Sources[0].Error != nil || !after.Sources[0].LastSuccessAt.Equal(*before.Sources[0].LastSuccessAt) || !after.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("reversing a pending disable changed recommendation caches", after)
	}
	if state = managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f"); state.Generation != generation || state.Reason != nil {
		t.Fatal("reversing a pending disable replaced the online worker", state)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/configuration", map[string]any{"name": "Renamed online"})
	waitLibraryOperation(t, c, operation.ID)
	authenticateManagedOnline(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f")
	after = managedRecommendations(t, c)
	if after.Sources[0].Error != nil || after.Sources[0].Name != "Fixture listener" || !after.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("configuration replacement lost the account name or another instance's cache", after)
	}
	if state = managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f"); state.Generation == generation || state.Name != "Fixture listener" {
		t.Fatal("configuration did not replace the worker while preserving its account name", state)
	}
	time.Sleep(1050 * time.Millisecond)
	type result struct {
		code int
		body string
		err  error
	}
	finished := make(chan result, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		request, err := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/online-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/search?q=slow&limit=1", nil)
		if err != nil {
			finished <- result{err: err}
			return
		}
		response, err := c.http.Do(request)
		if err != nil {
			finished <- result{err: err}
			return
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		finished <- result{code: response.StatusCode, body: string(raw), err: err}
	}()
	for deadline := time.Now().Add(3 * time.Second); ; {
		if state := managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f"); state.ActiveLeases > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow query did not acquire a runtime lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	state = managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f")
	if state.ActiveLeases == 0 || !state.AcceptingNewWork || state.RuntimeState != "ready" {
		t.Fatal("pending disable affected admitted work or new admission", state)
	}
	force := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/state", map[string]any{"enabled": false, "mode": "force"})
	waitLibraryOperation(t, c, force.ID)
	select {
	case done := <-finished:
		t.Fatal("legacy force mode canceled admitted work", done)
	case <-time.After(150 * time.Millisecond):
	}
	if state = managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f"); state.RuntimeState != "ready" || state.ActiveLeases == 0 || !state.AcceptingNewWork {
		t.Fatal("legacy force mode changed the online runtime", state)
	}
	after = managedRecommendations(t, c)
	if after.Sources[1].Error != nil || !after.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("pending disable affected another online instance", after.Sources[1])
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled client request did not finish")
	}
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	if state = managedOnlineInstance(t, c, "a0462a4b-8754-5747-a8cf-29cf4d0a874f"); state.RuntimeState != "disabled" || state.EffectiveEnabled || state.AcceptingNewWork || state.ActiveLeases != 0 {
		t.Fatal("server restart did not apply persisted disable", state)
	}
	code, raw, _ = c.request(t, "GET", "/api/v1/online-instances/a0462a4b-8754-5747-a8cf-29cf4d0a874f/search?q=Fixture", nil, "")
	var failure struct{ Error domain.Error }
	if code != 503 || json.Unmarshal(raw, &failure) != nil || failure.Error.Code != "PLUGIN_DISABLED" {
		t.Fatalf("disabled online worker accepted work after server restart: %d %s", code, raw)
	}
	if state = managedOnlineInstance(t, c, "18caca33-4b0c-57b7-8248-2cf8781c2448"); state.RuntimeState != "ready" || !state.EffectiveEnabled || !state.AcceptingNewWork {
		t.Fatal("server restart disabled another online instance", state)
	}
}

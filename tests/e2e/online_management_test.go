package e2e_test

import (
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
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
	code, raw, _ := c.request(t, "GET", "/api/v1/recommendations?limit=1", nil, "")
	var response recommendations.Response
	if code != 200 || json.Unmarshal(raw, &response) != nil || len(response.Sources) != 2 {
		t.Fatalf("recommendations unavailable: %d %s", code, raw)
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

func TestManagedOnlineWorkerRestartCacheIsolationAndForcedDrain(t *testing.T) {
	provider := managedOnlineFixture(t)
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.netease", Executable: provider, Enabled: true, Instances: []config.Instance{
		{InstanceID: "online-a", Name: "First", Role: "provider", Enabled: true},
		{InstanceID: "online-b", Name: "Second", Role: "provider", Enabled: true},
	}}}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer stop()
	c := login(t, base, cfg.AdminToken)
	authenticateManagedOnline(t, c, "online-a", "online-b")
	before := managedRecommendations(t, c)
	for _, source := range before.Sources {
		if source.Error != nil || len(source.Items) != 1 || source.LastSuccessAt == nil {
			t.Fatal("initial online worker was not ready", source)
		}
	}
	operation := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/online-a/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	disabled := managedRecommendations(t, c)
	if disabled.Sources[0].Error == nil || disabled.Sources[0].Error.Code != "PLUGIN_DISABLED" || len(disabled.Sources[0].Items) != 0 || !disabled.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("disabled instance reused stale cache or changed another instance", disabled)
	}
	state := managedOnlineInstance(t, c, "online-a")
	if state.RuntimeState != "disabled" || state.ActiveLeases != 0 || state.AcceptingNewWork {
		t.Fatal("online worker did not finish stopping", state)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/online-a/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	authenticateManagedOnline(t, c, "online-a")
	after := managedRecommendations(t, c)
	if after.Sources[0].Error != nil || !after.Sources[0].LastSuccessAt.After(*before.Sources[0].LastSuccessAt) || !after.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("replacement did not refresh only its own recommendation cache", after)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/online-a/configuration", map[string]any{"name": "Renamed online"})
	waitLibraryOperation(t, c, operation.ID)
	authenticateManagedOnline(t, c, "online-a")
	after = managedRecommendations(t, c)
	if after.Sources[0].Error != nil || after.Sources[0].Name != "Renamed online" || !after.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("online configuration was not reflected in replacement runtime", after)
	}
	time.Sleep(1050 * time.Millisecond)
	type result struct {
		code int
		body string
		err  error
	}
	finished := make(chan result, 1)
	go func() {
		response, err := c.http.Get(base + "/api/v1/online-instances/online-a/search?q=slow&limit=1")
		if err != nil {
			finished <- result{err: err}
			return
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		finished <- result{code: response.StatusCode, body: string(raw), err: err}
	}()
	for deadline := time.Now().Add(3 * time.Second); ; {
		if state := managedOnlineInstance(t, c, "online-a"); state.ActiveLeases > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow query did not acquire a runtime lease")
		}
		time.Sleep(10 * time.Millisecond)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/online-a/state", map[string]any{"enabled": false, "mode": "drain"})
	state = managedOnlineInstance(t, c, "online-a")
	if state.ActiveLeases == 0 || state.AcceptingNewWork {
		t.Fatal("drain lost admitted work or kept new admission open", state)
	}
	code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/online-a/search?q=Fixture", nil, "")
	if code != 503 || !strings.Contains(string(raw), "PLUGIN_DISABLED") {
		t.Fatalf("new online query entered drain: %d %s", code, raw)
	}
	force := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/online-a/state", map[string]any{"enabled": false, "mode": "force"})
	waitLibraryOperation(t, c, force.ID)
	select {
	case done := <-finished:
		if done.err != nil || done.code != 503 {
			t.Fatal("forced query did not terminate", done)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forced query did not release its worker")
	}
	if state = managedOnlineInstance(t, c, "online-a"); state.RuntimeState != "disabled" || state.ActiveLeases != 0 {
		t.Fatal("force completed before online cleanup", state)
	}
	after = managedRecommendations(t, c)
	if after.Sources[1].Error != nil || !after.Sources[1].LastSuccessAt.Equal(*before.Sources[1].LastSuccessAt) {
		t.Fatal("force affected another online instance", after.Sources[1])
	}
}

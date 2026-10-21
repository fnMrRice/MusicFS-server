package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineHTTPRelaysWorkerOwnedPluginIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "online-identity")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	build := exec.Command("go", "build", "-mod=readonly", "-o", path, "./tests/testkit/online-identity-worker")
	build.Dir = filepath.Join("..", "..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build identity fixture: %v %s", err, out)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := pluginhost.StartOnline(context.Background(), pluginhost.OnlineConfig{Executable: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), StateDir: t.TempDir(), Platform: online.Netease, UserID: "admin", InstanceID: "ba8e4265-7617-542b-8ac7-fa5fbdff5c2e"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	server, _, _ := libraryHTTP(t)
	cookie, _ := login(t, server)
	r := onlinehost.Runtime{Owner: "admin", InstanceID: "ba8e4265-7617-542b-8ac7-fa5fbdff5c2e", Name: "Operator instance label", Platform: online.Netease, ConfigRevision: "r1", Enabled: true, Provider: worker}
	service, err := onlinehost.New([]onlinehost.Runtime{r, {Owner: "admin", InstanceID: "5c49ff37-ff10-5500-837a-93479e4d7528", Platform: online.Netease, ConfigRevision: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	verify := func() {
		t.Helper()
		request := httptest.NewRequest("GET", "/api/v1/online-instances", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("instances status: %d %s", response.Code, response.Body.String())
		}
		testkit.ValidateHTTP(t, "OnlineInstances", response.Body.Bytes())
		var payload struct {
			Items []onlinehost.Instance `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Items) != 2 {
			t.Fatal("unexpected instances", payload)
		}
		found := false
		for _, item := range payload.Items {
			if item.InstanceID != r.InstanceID {
				if item.Plugin != nil {
					t.Fatal("never-started instance invented plugin metadata")
				}
				continue
			}
			found = true
			plugin := item.Plugin
			if plugin == nil || plugin.ID != "musicfs.plugin.source.netease" || plugin.Name != "Fixture Music" || plugin.LocalizedNames["zh-Hans"] != "测试音乐" || plugin.LocalizedNames["fr"] != "Musique de test" || len(plugin.Features) != 1 || plugin.Features[0] != "music-source" || item.Name != plugin.Name {
				t.Fatal("worker metadata was not preserved", item)
			}
		}
		if !found {
			t.Fatal("worker instance missing")
		}
	}
	verify()
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	r.Provider, r.Enabled, r.ConfigRevision = nil, false, "r2"
	if err := service.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	verify()
}

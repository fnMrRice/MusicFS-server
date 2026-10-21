package plugincontrol_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPluginManagementHTTPVerifiedVersionMetadata(t *testing.T) {
	manager, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{
		{PluginID: "musicfs.plugin.decoder.ncm", Instances: []config.Instance{{InstanceID: ncmInstanceID}}},
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	server, cookies := managementHTTP(t, manager)
	version := ""
	if err := manager.Register(ncmInstanceID, plugincontrol.Hooks{
		PluginVersion: func() string { return version },
	}, false); err != nil {
		t.Fatal(err)
	}
	for _, current := range []string{"", " ", "2.3.4-rc.1+fixture"} {
		version = current
		response := managementRequest(server, cookies["admin"], "GET", "/admin/plugins", "", "", "")
		requireHTTP(t, response, 200, "")
		testkit.ValidateHTTP(t, "PluginManagementSnapshot", response.Body.Bytes())
		var snapshot struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		got, present := snapshot.Items[0]["version"]
		want := current
		if current == "" || current == " " {
			want = bundledplugin.NCMVersion
		}
		if !present || got != want {
			t.Fatal("HTTP response lost the declared or verified plugin version", got)
		}
		for _, invalid := range []any{nil, "", " ", "1.0", "1.0.0-01"} {
			if invalid == nil {
				delete(snapshot.Items[0], "version")
			} else {
				snapshot.Items[0]["version"] = invalid
			}
			if testkit.ContractSchema(t, "http", "ManagedPlugin").Validate(snapshot.Items[0]) == nil {
				t.Fatalf("HTTP contract accepted invalid plugin version: %v", invalid)
			}
		}
	}
}

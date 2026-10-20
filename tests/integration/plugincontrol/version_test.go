package plugincontrol_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
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
	for _, current := range []string{"", "2.3.4-rc.1+fixture"} {
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
		if current == "" && present {
			t.Fatal("HTTP response invented an unknown version", got)
		}
		if current != "" && got != current {
			t.Fatal("HTTP response lost the verified plugin version", got)
		}
	}
}

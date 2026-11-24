package plugincontrol_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRootImportDefaultsPersistWithoutChangingOtherDirectories(t *testing.T) {
	manager, allowed := configurationManager(t)
	server, cookies := managementHTTP(t, manager)
	path := "/admin/plugin-instances/" + localInstanceID + "/configuration"
	child := filepath.Join(allowed, "Collection")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	initialCount := manager.SnapshotForScope(context.Background(), "admin", false).Items[0].DisplayCount.Value
	options := func(recursive, hidden bool) map[string]any {
		return map[string]any{"recursive": recursive, "maxDepth": 0, "importLyrics": false, "importArtwork": hidden, "excludePatterns": []string{}, "skipHidden": hidden, "respectNoMedia": false}
	}
	save := func(index int, roots []map[string]any, status int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"musicRoots": roots})
		w := managementRequest(server, cookies["admin"], "PUT", path, string(raw), manager.Snapshot().Revision, fmt.Sprintf("save-root-options-%d", index))
		requireHTTP(t, w, status, "")
	}
	read := func() plugincontrol.Configuration {
		t.Helper()
		w := managementRequest(server, cookies["admin"], "GET", path, "", "", "")
		requireHTTP(t, w, 200, "")
		testkit.ValidateHTTP(t, "ManagedPluginConfiguration", w.Body.Bytes())
		var view plugincontrol.Configuration
		if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		return view
	}
	save(1, []map[string]any{
		{"id": "music", "name": "Flat", "importDefaults": options(false, false)},
		{"id": "child", "name": "Recursive", "path": child, "importDefaults": options(true, true)},
	}, 202)
	view := read()
	if len(view.Roots) != 2 || *view.Roots[0].ImportDefaults.Recursive || !*view.Roots[1].ImportDefaults.Recursive || *view.Roots[0].ImportDefaults.SkipHidden || !*view.Roots[1].ImportDefaults.SkipHidden || *view.Roots[0].ImportDefaults.RespectNoMedia || *view.Roots[0].ImportDefaults.ImportArtwork || !*view.Roots[1].ImportDefaults.ImportArtwork {
		t.Fatal("directory configuration was not independently projected", view.Roots)
	}
	if !view.ImportOptions.ImportDefaults.Recursive || !view.ImportOptions.ImportDefaults.RespectNoMedia {
		t.Fatal("root edit changed instance defaults")
	}
	// 仅重命名目录时保留此前保存的覆盖值。
	save(2, []map[string]any{{"id": "music", "name": "Renamed"}, {"id": "child", "name": "Recursive", "importDefaults": options(false, true)}}, 202)
	view = read()
	if view.Roots[0].Name != "Renamed" || *view.Roots[0].ImportDefaults.Recursive || *view.Roots[1].ImportDefaults.Recursive || *view.Roots[0].ImportDefaults.SkipHidden {
		t.Fatal("editing one root reset another root")
	}
	loaded, err := config.Load(manager.RuntimeConfig().SourcePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range loaded.Plugins {
		if plugin.PluginID != "musicfs.plugin.source.localfiles" {
			continue
		}
		roots := plugin.Instances[0].Config.MusicRoots
		if len(roots) != 2 || roots[0].ImportDefaults == nil || *roots[0].ImportDefaults.Recursive || roots[1].ImportDefaults == nil || *roots[1].ImportDefaults.Recursive {
			t.Fatal("restart did not retain root settings")
		}
	}
	bad := options(false, true)
	bad["maxDepth"] = 1
	save(3, []map[string]any{{"id": "music", "name": "Invalid", "importDefaults": bad}}, 400)
	if len(read().Roots) != 2 {
		t.Fatal("invalid root options mutated persisted configuration")
	}
	save(4, []map[string]any{}, 202)
	if len(read().Roots) != 0 {
		t.Fatal("removal kept configured roots")
	}
	if count := manager.SnapshotForScope(context.Background(), "admin", false).Items[0].DisplayCount; count == nil || count.Value != initialCount {
		t.Fatal("removing selected roots changed deployment permission count", count)
	}
	save(5, []map[string]any{{"id": "inherited", "name": "Inherited", "path": child}}, 202)
	inherited := read().Roots[0].ImportDefaults
	if !*inherited.Recursive || !*inherited.SkipHidden || !*inherited.RespectNoMedia || !*inherited.ImportArtwork {
		t.Fatal("new root without overrides did not inherit instance defaults")
	}
}

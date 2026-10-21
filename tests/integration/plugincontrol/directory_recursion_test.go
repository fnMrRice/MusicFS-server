package plugincontrol_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestDirectoryManagementSavesRecursiveDefaults(t *testing.T) {
	manager, _ := configurationManager(t)
	server, cookies := managementHTTP(t, manager)
	path := "/admin/plugin-instances/" + localInstanceID + "/configuration"
	for index, options := range []pluginproto.ImportOptions{
		{Recursive: true, MaxDepth: 2, ExcludePatterns: []string{"cache"}, SkipHidden: true, RespectNoMedia: true},
		{Recursive: true, MaxDepth: 0, ExcludePatterns: []string{"cache"}},
		{Recursive: false, MaxDepth: 0, ExcludePatterns: []string{"cache"}, SkipHidden: true, RespectNoMedia: false},
	} {
		raw, err := json.Marshal(map[string]any{"importDefaults": map[string]any{"recursive": options.Recursive, "maxDepth": options.MaxDepth, "importLyrics": options.ImportLyrics, "excludePatterns": options.ExcludePatterns, "skipHidden": options.SkipHidden, "respectNoMedia": options.RespectNoMedia}})
		if err != nil {
			t.Fatal(err)
		}
		revision := `"` + manager.Snapshot().Revision + `"`
		w := managementRequest(server, cookies["admin"], "PUT", path, string(raw), revision, fmt.Sprintf("save-recursion-%d", index))
		requireHTTP(t, w, 202, "")
		testkit.ValidateHTTP(t, "PluginOperation", w.Body.Bytes())
		w = managementRequest(server, cookies["admin"], "GET", path, "", "", "")
		requireHTTP(t, w, 200, "")
		testkit.ValidateHTTP(t, "ManagedPluginConfiguration", w.Body.Bytes())
		var view plugincontrol.Configuration
		if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.ImportOptions == nil || view.ImportOptions.SchemaRevision != "local-import-v5" {
			t.Fatal("new recursion declaration missing")
		}
		actual := view.ImportOptions.ImportDefaults
		if actual.Recursive != options.Recursive || actual.MaxDepth != options.MaxDepth || actual.SkipHidden != options.SkipHidden || actual.RespectNoMedia != options.RespectNoMedia || len(actual.ExcludePatterns) != 1 || actual.ExcludePatterns[0] != "cache" {
			t.Fatal("saved recursion or existing exclusions changed")
		}
		if len(view.Roots) != 1 {
			t.Fatal("changing recursion removed selected directories")
		}
	}
}

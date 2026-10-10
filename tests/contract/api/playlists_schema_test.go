package api_test

import (
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"testing"
)

func TestPlaylistsOpenAPIOperations(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	operations := map[string]bool{}
	for _, path := range doc["paths"].(map[string]any) {
		for method, value := range path.(map[string]any) {
			if method != "parameters" {
				operations[value.(map[string]any)["operationId"].(string)] = true
			}
		}
	}
	for _, id := range []string{"createPlaylist", "listPlaylists", "getPlaylist", "editPlaylist", "rebuildPlaylist", "getPlaylistSnapshot", "getPlaylistHiddenRules", "hidePlaylistSource", "changePlaylistHidden", "listPlaylistChanges"} {
		if !operations[id] {
			t.Errorf("missing playlist operation %s", id)
		}
	}
}

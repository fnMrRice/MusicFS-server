package api_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRemotePlaylistsOpenAPIOperations(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	operations := map[string]bool{}
	for _, path := range doc["paths"].(map[string]any) {
		for method, value := range path.(map[string]any) {
			if method != "parameters" {
				operations[value.(map[string]any)["operationId"].(string)] = true
			}
		}
	}
	for _, id := range []string{"importRemotePlaylist", "listRemotePlaylists", "getRemotePlaylist", "syncRemotePlaylist", "getRemotePlaylistSync", "cancelRemotePlaylistSync", "getRemotePlaylistSnapshot"} {
		if !operations[id] {
			t.Errorf("missing remote playlist operation %s", id)
		}
	}
}

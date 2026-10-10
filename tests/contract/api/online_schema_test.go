package api_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineOpenAPIOperations(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	operations := map[string]bool{}
	for _, path := range doc["paths"].(map[string]any) {
		for method, value := range path.(map[string]any) {
			if method != "parameters" {
				operations[value.(map[string]any)["operationId"].(string)] = true
			}
		}
	}
	for _, operation := range []string{"listOnlineInstances", "searchOnlineMetadata", "getOnlineTrack", "observeOnlinePlaylist", "getOnlineRecommendations", "resolveOnlineTrack", "lookupOnlineTracks", "listOnlinePlaylistTracks", "lookupSourceReferences", "getOnlineAlbum", "getOnlineArtist", "listOnlineAlbumTracks", "listOnlineArtistAlbums", "listOnlineArtistTracks"} {
		if !operations[operation] {
			t.Errorf("missing online operation %s", operation)
		}
	}
}

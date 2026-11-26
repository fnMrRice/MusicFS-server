package api_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAlbumOpenAPIOperations(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	operations := map[string]bool{}
	for _, path := range doc["paths"].(map[string]any) {
		for method, value := range path.(map[string]any) {
			if method != "parameters" {
				operations[value.(map[string]any)["operationId"].(string)] = true
			}
		}
	}
	for _, family := range []struct{ kind, plural string }{{"SourceAlbum", "SourceAlbums"}, {"Release", "Releases"}} {
		for _, operation := range []string{"list" + family.plural, "create" + family.kind, "get" + family.kind, "update" + family.kind, "get" + family.kind + "Cover", "select" + family.kind + "Cover", "unlock" + family.kind + "Cover", "list" + family.kind + "Changes"} {
			if !operations[operation] {
				t.Errorf("missing operation %s", operation)
			}
		}
	}
	for _, operation := range []string{"createThumbnail", "listScrapeDocuments", "getMusicAlbumArtwork", "refreshMusicAlbumArtwork"} {
		if !operations[operation] {
			t.Errorf("missing operation %s", operation)
		}
	}
}

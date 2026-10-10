package api_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestDirectoryBrowsingOpenAPIContract(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	path, ok := doc["paths"].(map[string]any)["/admin/plugin-instances/{id}/directories"].(map[string]any)
	if !ok || path["get"].(map[string]any)["operationId"] != "listManagedPluginDirectories" {
		t.Fatal("directory browsing operation missing")
	}
	schema := testkit.ContractSchema(t, "http", "ManagedPluginDirectories")
	for _, raw := range []string{
		`{"revision":"r","path":null,"parentPath":null,"configuredRootId":null,"items":[{"name":"Music","path":"/home/user/Music","available":false,"configuredRootId":"music"}],"nextCursor":null}`,
		`{"revision":"r","path":"/home/user/Music","parentPath":null,"configuredRootId":"music","items":[],"nextCursor":"next-page"}`,
	} {
		if err := schema.Validate(testkit.DecodeJSON(t, []byte(raw))); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"revision":"r","path":null,"parentPath":null,"items":[{"name":"Music","path":"/Music"}],"nextCursor":null}`,
		`{"revision":"r","path":null,"parentPath":null,"items":[],"nextCursor":null,"files":["private-song.wav"]}`,
	} {
		if schema.Validate(testkit.DecodeJSON(t, []byte(raw))) == nil {
			t.Fatal("invalid directory response accepted")
		}
	}
}

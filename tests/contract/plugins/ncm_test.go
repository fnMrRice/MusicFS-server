package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestNCMWireContract(t *testing.T) {
	nonce := strings.Repeat("a", 48)
	request := func() map[string]any {
		return map[string]any{"taskId": "task-1", "assetId": "asset-1", "nonce": nonce, "inputUrl": "http://127.0.0.1:32123/" + nonce + "/input", "size": 12345}
	}
	metadata := ncmdecoder.Metadata{Format: "mp3", MIME: "audio/mpeg", Title: "Synthetic"}
	result := func() map[string]any {
		return map[string]any{"taskId": "task-1", "assetId": "asset-1", "nonce": nonce, "outputUrl": "http://127.0.0.1:32124/" + nonce + "/output", "size": 12000, "metadata": metadata}
	}
	testkit.ValidatePlugin(t, "NCMMetadata", metadata)
	testkit.ValidatePlugin(t, "NCMOpen", request())
	testkit.ValidatePlugin(t, "NCMOpened", result())
	testkit.ValidatePlugin(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": "ncm.open", "params": request()})
	testkit.ValidatePlugin(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "result": result()})
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { delete(v, "assetId") },
		func(v map[string]any) { v["nonce"] = "short" },
		func(v map[string]any) { v["taskId"] = "../task" },
		func(v map[string]any) { v["inputUrl"] = "https://example.com/private.ncm" },
		func(v map[string]any) { v["inputUrl"] = "http://127.0.0.1:32123/" + nonce + "/output" },
		func(v map[string]any) { v["inputUrl"] = "http://127.0.0.1:32123/" + nonce + "/input?other=1" },
		func(v map[string]any) { v["size"] = 0 },
		func(v map[string]any) { v["sourcePath"] = "private.ncm" },
		func(v map[string]any) { v["body"] = "forbidden base64 audio" },
	} {
		value := request()
		mutate(value)
		assertNCMInvalid(t, "NCMOpen", value)
		assertNCMInvalid(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": "ncm.open", "params": value})
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { delete(v, "taskId") },
		func(v map[string]any) { v["size"] = -1 },
		func(v map[string]any) { v["metadata"] = map[string]any{"format": "mp3", "mime": "audio/flac"} },
		func(v map[string]any) { v["metadata"] = map[string]any{"format": "aac", "mime": "audio/aac"} },
		func(v map[string]any) { v["outputUrl"] = "http://localhost:32124/" + nonce + "/output" },
		func(v map[string]any) { v["cover"] = "forbidden image" },
	} {
		value := result()
		mutate(value)
		assertNCMInvalid(t, "NCMOpened", value)
	}
}

func assertNCMInvalid(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if testkit.ContractSchema(t, "plugin", name).Validate(testkit.DecodeJSON(t, body)) == nil {
		t.Fatalf("invalid NCM model accepted for %s: %s", name, body)
	}
}

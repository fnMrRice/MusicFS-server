package api_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRecommendationsOpenAPIOperation(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	path := doc["paths"].(map[string]any)["/recommendations"].(map[string]any)
	get := path["get"].(map[string]any)
	if get["operationId"] != "listRecommendations" {
		t.Fatal("missing grouped recommendation operation")
	}
	parameters := get["parameters"].([]any)
	for _, raw := range parameters {
		parameter := raw.(map[string]any)
		if parameter["name"] == "instanceId" {
			if parameter["style"] != "form" || parameter["explode"] != true {
				t.Fatal("recommendation instances must use repeated query parameters")
			}
			return
		}
	}
	t.Fatal("missing instance selector")
}

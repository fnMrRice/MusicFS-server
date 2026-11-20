package api_test

import (
	"encoding/json"
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
	seenInstances, seenHistory := false, false
	for _, raw := range parameters {
		parameter := raw.(map[string]any)
		switch parameter["name"] {
		case "instanceId":
			if parameter["style"] != "form" || parameter["explode"] != true {
				t.Fatal("recommendation instances must use repeated query parameters")
			}
			seenInstances = true
		case "history":
			schema := parameter["schema"].(map[string]any)
			if schema["type"] != "integer" || schema["minimum"] != json.Number("0") || schema["maximum"] != json.Number("5") || schema["default"] != json.Number("0") {
				t.Fatal("history selector must document current and five prior pages")
			}
			seenHistory = true
		}
	}
	if !seenInstances || !seenHistory {
		t.Fatal("missing recommendation selector")
	}
}

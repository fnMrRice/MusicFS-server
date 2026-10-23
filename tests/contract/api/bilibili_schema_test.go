package api_test

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestProviderContractExposesDeclaredSourceType(t *testing.T) {
	provider := library.Provider{ID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", Name: "Custom source", Availability: "available", AccountState: "notRequired", ScanState: "idle", Capabilities: []string{}, Roots: []library.ProviderRoot{}}
	for _, source := range []string{"local", "network", "", "bilibili"} {
		provider.SourceType = source
		body, _ := json.Marshal(provider)
		err := testkit.ContractSchema(t, "http", "Provider").Validate(testkit.DecodeJSON(t, body))
		if (err == nil) != (source == "local" || source == "network" || source == "") {
			t.Fatalf("source=%q provider contract mismatch: %v", source, err)
		}
		if source == "" {
			value := testkit.DecodeJSON(t, body).(map[string]any)
			if _, exists := value["sourceType"]; exists {
				t.Fatal("unknown source type was not omitted")
			}
			value["sourceType"] = ""
			if testkit.ContractSchema(t, "http", "Provider").Validate(value) == nil {
				t.Fatal("explicit empty source type accepted")
			}
		}
	}
}

func TestBilibiliOpenAPIResolveAndNamespaces(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	paths := doc["paths"].(map[string]any)
	base := "/online-instances/{id}"
	resolve := paths[base+"/tracks/{externalId}/resolve"].(map[string]any)["post"].(map[string]any)
	response, ok := resolve["responses"].(map[string]any)["200"].(map[string]any)
	if !ok {
		t.Fatal("online resolve does not declare a success response")
	}
	schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["$ref"] != "./http.schema.json#/$defs/SourceTrack" {
		t.Fatal("online resolve did not return the playback source contract")
	}
	for _, fixture := range []struct {
		path   string
		valid  []string
		reject []string
	}{
		{base + "/tracks/{externalId}", []string{"video:BV13x41117TL:101", "audio:10"}, []string{"unavailable:2:500", "audio:01", "favorite:10"}},
		{base + "/playlists/{externalId}", []string{"favorite:10", "audio-menu:10"}, []string{"favorite:01", "video:BV13x41117TL:101", "https://example.invalid/list"}},
	} {
		item := paths[fixture.path].(map[string]any)
		var pattern string
		for _, parameter := range item["parameters"].([]any) {
			value := parameter.(map[string]any)
			if value["name"] == "externalId" {
				pattern = value["schema"].(map[string]any)["pattern"].(string)
			}
		}
		if pattern == "" {
			t.Fatal("external identity pattern is missing", fixture.path)
		}
		compiled := regexp.MustCompile(pattern)
		for _, id := range fixture.valid {
			if !compiled.MatchString(id) {
				t.Fatal("canonical identity rejected", id)
			}
		}
		for _, id := range fixture.reject {
			if compiled.MatchString(id) {
				t.Fatal("invalid identity accepted", id)
			}
		}
	}
	testkit.ValidateHTTP(t, "Error", []byte(`{"error":{"code":"RISK_CONTROL","message":"Upstream risk control rejected the request","requestId":"risk-fixture","retryAfter":null}}`))
}

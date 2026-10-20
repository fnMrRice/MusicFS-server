package api_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/instanceid"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func fixtureInstanceID(name string) string {
	return instanceid.Canonical(name)
}

func TestCurrentInstanceRequestsUseUUIDWhileHistoricalOutputsRemainReadable(t *testing.T) {
	validate := func(model string, value any) error {
		t.Helper()
		body, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return testkit.ContractSchema(t, "http", model).Validate(testkit.DecodeJSON(t, body))
	}
	current := map[string]any{
		"sourceTrackIds": []string{"source"}, "fields": []string{"lyrics"},
		"instanceIds": []string{"legacy-scraper"},
	}
	if err := validate("CreateMetadataRequest", current); err == nil {
		t.Fatal("current metadata request accepted a legacy instance ID")
	}
	current["instanceIds"] = []string{fixtureInstanceID("legacy-scraper")}
	if err := validate("CreateMetadataRequest", current); err != nil {
		t.Fatal("current metadata request rejected a UUID", err)
	}
	current["instanceIds"] = []string{"legacy-scraper"}
	current["excludeInstanceIds"] = []string{"other-legacy"}
	current["languages"] = []string{}
	current["query"] = nil
	if err := validate("MetadataRequest", current); err != nil {
		t.Fatal("historical metadata request became unreadable", err)
	}
	for _, id := range []string{"legacy-scraper", fixtureInstanceID("legacy-scraper"), ""} {
		origin := map[string]any{"kind": "scraper", "instanceId": id, "candidateId": "candidate"}
		if err := validate("ContentOrigin", origin); err != nil {
			t.Fatal("historical content origin became unreadable", err)
		}
		operation := map[string]any{
			"operationId": "op", "revision": "r", "pluginId": "musicfs.scraper.local", "instanceId": id,
			"mode": "drain", "status": "succeeded", "errorCode": nil, "restartRequired": false,
			"createdAt": "2026-10-20T00:00:00Z", "updatedAt": "2026-10-20T00:00:00Z",
		}
		if err := validate("PluginOperation", operation); err != nil {
			t.Fatal("historical plugin operation became unreadable", err)
		}
	}
}

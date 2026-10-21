package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestDSPTimelineProfileContract(t *testing.T) {
	profile := timeline.Profile{OutputProfile: "mp3-128", ProcessingRevision: "dsp-v1:" + strings.Repeat("a", 64), DecoderSHA256: strings.Repeat("b", 64), GainDB: -6}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "TimelineProfile", raw)
	for _, revision := range []string{"dsp-v1", "dsp-v1:" + strings.Repeat("G", 64), "dsp-v2:" + strings.Repeat("a", 64)} {
		value := testkit.DecodeJSON(t, raw).(map[string]any)
		value["processingRevision"] = revision
		if testkit.ContractSchema(t, "http", "TimelineProfile").Validate(value) == nil {
			t.Fatal("malformed DSP chain revision passed HTTP contract")
		}
	}
}

func TestDSPProcessingDescriptionContract(t *testing.T) {
	samples := testkit.ReadJSON(t, "tests/testdata/contracts/api/samples.json").([]any)
	for _, item := range samples {
		sample := item.(map[string]any)
		if sample["name"] != "playback" {
			continue
		}
		value := sample["value"].(map[string]any)
		raw, err := json.Marshal([]playback.Processing{
			{Type: "gain", GainDB: -3, Version: "1", Execution: "server"},
			{Type: "channelmix", Version: "2", Execution: "server", Parameters: map[string]float64{"width": .5}},
		})
		if err != nil {
			t.Fatal(err)
		}
		value["effectiveProcessing"] = testkit.DecodeJSON(t, raw)
		if err = testkit.ContractSchema(t, "http", "Playback").Validate(value); err != nil {
			t.Fatal(err)
		}
		value["effectiveProcessing"].([]any)[1].(map[string]any)["parameters"] = map[string]any{"width": "not-a-number"}
		if testkit.ContractSchema(t, "http", "Playback").Validate(value) == nil {
			t.Fatal("non-numeric DSP parameters passed HTTP contract")
		}
		return
	}
	t.Fatal("playback fixture is missing")
}

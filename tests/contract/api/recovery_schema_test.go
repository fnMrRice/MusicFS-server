package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestTimelineAndRecoveryDTOContracts(t *testing.T) {
	profile := timeline.Profile{OutputProfile: "mp3-128", ProcessingRevision: "gain-v1", DecoderSHA256: strings.Repeat("a", 64), GainDB: 0}
	r := timeline.Request{RecordingID: "recording", From: timeline.Target{SourceTrackID: "source-a", AssetID: "asset-a", Version: "a-v1"}, To: timeline.Target{SourceTrackID: "source-b", AssetID: "asset-b", Version: "b-v1"}, Range: timeline.Range{EndMs: 6000}, OffsetMs: 1000, Profile: profile}
	e := timeline.Evidence{ID: strings.Repeat("b", 48), Request: r, Origin: "manualAlignment", Revision: 1, CreatedAt: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}
	for _, row := range []struct {
		name  string
		value any
	}{
		{"TimelineProfile", profile}, {"CreateTimelineEvidence", r}, {"TimelineEvidence", e},
		{"RecoverSource", playback.RecoverSourceRequest{AttemptID: "attempt", TargetSourceTrackID: "source-b", PositionMs: 4000, EvidenceID: &e.ID, ObservedError: &playback.ObservedError{Code: "NETWORK", Message: "Interrupted"}}},
		{"RecoveryResult", playback.RecoveryResult{Decision: "promptRestart", Reason: "TIMELINE_EVIDENCE_REQUIRED", TargetSourceTrackID: "source-b"}},
	} {
		b, err := json.Marshal(row.value)
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, row.name, b)
	}
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["origin"] = "fingerprint" },
		func(v map[string]any) { v["owner"] = "forged-owner" },
		func(v map[string]any) { v["from"].(map[string]any)["path"] = "/private/audio.wav" },
		func(v map[string]any) { delete(v, "offsetMs") },
		func(v map[string]any) { v["profile"].(map[string]any)["decoderSha256"] = "unknown" },
	} {
		b, _ := json.Marshal(e)
		v := testkit.DecodeJSON(t, b).(map[string]any)
		change(v)
		if testkit.ContractSchema(t, "http", "TimelineEvidence").Validate(v) == nil {
			t.Fatal("invalid or private alignment evidence accepted")
		}
	}
}

func TestRecoveryOpenAPIOperations(t *testing.T) {
	doc := testkit.ReadJSON(t, "docs/contracts/openapi.yaml").(map[string]any)
	paths := doc["paths"].(map[string]any)
	for _, expected := range []struct{ path, method, id string }{
		{"/playback/recovery-profile", "get", "getRecoveryProfile"},
		{"/timeline-evidence", "post", "createTimelineEvidence"},
		{"/timeline-evidence/{id}", "get", "getTimelineEvidence"},
		{"/timeline-evidence/{id}/revoke", "post", "revokeTimelineEvidence"},
		{"/playback/sessions/{id}/recover-source", "post", "recoverPlaybackSource"},
	} {
		path, ok := paths[expected.path].(map[string]any)
		if !ok {
			t.Fatalf("missing recovery path %s", expected.path)
		}
		op, ok := path[expected.method].(map[string]any)
		if !ok || op["operationId"] != expected.id {
			t.Fatalf("missing recovery operation %s", expected.id)
		}
	}
}

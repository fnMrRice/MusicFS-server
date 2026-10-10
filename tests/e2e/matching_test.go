package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMatchingActualServerManualRoundTripRestartAndPlayback(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	for _, name := range []string{"one.wav", "two.wav"} {
		if err := os.WriteFile(filepath.Join(music, name), wave, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Matching fixture", Path: music}}}}}}}
	var sources []string
	var originalRecording, newRecording, linkOperation string
	t.Run("manual-round-trip", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		tracks := scanNCM(t, c)
		if len(tracks) != 2 {
			t.Fatal("missing independent sources")
		}
		sources = []string{tracks["one.wav"].ID, tracks["two.wav"].ID}
		for _, id := range sources {
			code, raw, _ := c.request(t, "POST", "/api/v1/source-tracks/"+id+"/hash-jobs", struct{}{}, "matching-hash-"+id)
			var accepted struct {
				JobID string `json:"jobId"`
			}
			if code != 202 || json.Unmarshal(raw, &accepted) != nil {
				t.Fatalf("hash submit: %d %s", code, raw)
			}
			for deadline := time.Now().Add(10 * time.Second); ; {
				code, raw, _ = c.request(t, "GET", "/api/v1/hash-jobs/"+accepted.JobID, nil, "")
				var job identification.HashJob
				if code != 200 || json.Unmarshal(raw, &job) != nil {
					t.Fatalf("hash status: %d %s", code, raw)
				}
				if job.Status == "succeeded" {
					break
				}
				if job.Status != "queued" && job.Status != "running" || time.Now().After(deadline) {
					t.Fatalf("hash failed: %s", raw)
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		refreshPath := "/api/v1/source-tracks/" + sources[0] + "/match-candidates/refresh"
		pool := map[string]any{"sourceTrackIds": []string{sources[1]}}
		code, raw, _ := c.request(t, "POST", refreshPath, pool, "")
		var candidates matching.CandidatePage
		if code != 200 || json.Unmarshal(raw, &candidates) != nil || len(candidates.Items) != 1 || candidates.Items[0].Score != 100 || !candidates.Items[0].RequiresConfirmation || candidates.Items[0].Reasons[0].Kind != "verifiedFileSHA512Equal" {
			t.Fatalf("verified-hash candidate: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "MatchCandidates", raw)
		mutate := func(path string, body any, key string) matching.Mutation {
			t.Helper()
			code, raw, _ := c.request(t, "POST", "/api/v1/matching/"+path, body, key)
			var out matching.Mutation
			if code != 200 || json.Unmarshal(raw, &out) != nil {
				t.Fatalf("%s: %d %s", path, code, raw)
			}
			testkit.ValidateHTTP(t, "MatchingMutation", raw)
			return out
		}
		joined := mutate("links", matching.LinkRequest{SourceTrackIDs: sources}, "matching-manual-link")
		if joined.RecordingID == nil || joined.Revision != 1 {
			t.Fatal("recording not created")
		}
		originalRecording, linkOperation = *joined.RecordingID, joined.OperationID
		mutate("unlinks", matching.UnlinkRequest{ExpectedRevision: 1, SourceTrackID: sources[1]}, "matching-manual-unlink")
		pair := matching.ExclusionRequest{ExpectedRevision: 2, LeftSourceTrackID: sources[0], RightSourceTrackID: sources[1], Excluded: true}
		mutate("exclusions", pair, "matching-manual-exclude")
		code, raw, _ = c.request(t, "POST", refreshPath, pool, "")
		if code != 200 || json.Unmarshal(raw, &candidates) != nil || len(candidates.Items) != 0 {
			t.Fatalf("excluded pair offered: %d %s", code, raw)
		}
		pair.ExpectedRevision, pair.Excluded = 3, false
		mutate("exclusions", pair, "matching-manual-unexclude")
		mutate("links", matching.LinkRequest{ExpectedRevision: 4, SourceTrackIDs: []string{sources[1]}, TargetRecordingID: &originalRecording}, "matching-manual-rejoin")
		split := mutate("splits", matching.SplitRequest{ExpectedRevision: 5, RecordingID: originalRecording, SourceTrackIDs: []string{sources[1]}}, "matching-manual-split")
		if split.Revision != 6 || split.RecordingID == nil || len(split.Changes) != 1 || split.Changes[0].FromRecordingID == nil || *split.Changes[0].FromRecordingID != originalRecording {
			t.Fatal("split audit is not reversible")
		}
		newRecording = *split.RecordingID
		request := playback.CreateRequest{SourceTrackID: sources[1], Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
		code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "matching-playback")
		var description playback.Description
		if code != 201 || json.Unmarshal(raw, &description) != nil {
			t.Fatalf("post-split playback: %d %s", code, raw)
		}
		verifyRanges(t, c, description, wave)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
			t.Fatal("stop failed")
		}
	})
	t.Run("restart", func(t *testing.T) {
		if len(sources) != 2 || newRecording == "" {
			t.Fatal("initial operation sequence failed")
		}
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		for i, id := range []string{originalRecording, newRecording} {
			code, raw, _ := c.request(t, "GET", "/api/v1/recordings/"+id, nil, "")
			var recording matching.Recording
			if code != 200 || json.Unmarshal(raw, &recording) != nil || recording.Revision != 6 || len(recording.SourceTrackIDs) != 1 || recording.SourceTrackIDs[0] != sources[i] {
				t.Fatalf("persisted membership: %d %s", code, raw)
			}
			testkit.ValidateHTTP(t, "Recording", raw)
		}
		code, raw, _ := c.request(t, "POST", "/api/v1/matching/links", matching.LinkRequest{SourceTrackIDs: sources}, "matching-manual-link")
		var replay matching.Mutation
		if code != 200 || json.Unmarshal(raw, &replay) != nil || replay.Revision != 1 || replay.OperationID != linkOperation {
			t.Fatalf("persisted idempotency: %d %s", code, raw)
		}
		code, raw, _ = c.request(t, "GET", "/api/v1/matching/changes", nil, "")
		var changes matching.ChangePage
		if code != 200 || json.Unmarshal(raw, &changes) != nil || changes.Revision != 6 || len(changes.Items) != 7 {
			t.Fatalf("persisted audit: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "MatchingChanges", raw)
		for _, name := range []string{"one.wav", "two.wav"} {
			actual, err := os.ReadFile(filepath.Join(music, name))
			if err != nil || !bytes.Equal(actual, wave) {
				t.Fatal("manual matching mutated source file")
			}
		}
	})
}

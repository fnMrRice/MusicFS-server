package e2e_test

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func submitHash(t *testing.T, c *client, track, key string) string {
	t.Helper()
	code, raw, _ := c.request(t, "POST", "/api/v1/source-tracks/"+track+"/hash-jobs", struct{}{}, key)
	if code != 202 {
		t.Fatalf("hash submission: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "HashAccepted", raw)
	var accepted struct {
		ID string `json:"jobId"`
	}
	if err := json.Unmarshal(raw, &accepted); err != nil {
		t.Fatal(err)
	}
	return accepted.ID
}

func readHash(t *testing.T, c *client, id string) identification.HashJob {
	t.Helper()
	code, raw, header := c.request(t, "GET", "/api/v1/hash-jobs/"+id, nil, "")
	if code != 200 {
		t.Fatalf("hash status: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "HashJob", raw)
	if bytes.Contains(raw, []byte("relativePath")) || bytes.Contains(raw, []byte("stateDir")) {
		t.Fatal("hash response leaked filesystem detail")
	}
	var job identification.HashJob
	if json.Unmarshal(raw, &job) != nil || job.Progress.CompletedBytes > job.Progress.TotalBytes || job.Error != nil && job.Error.RequestID != header.Get("X-Request-ID") {
		t.Fatalf("invalid hash job: %s", raw)
	}
	return job
}

func waitHash(t *testing.T, c *client, id string) identification.HashJob {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; {
		job := readHash(t, c, id)
		if job.Status != "queued" && job.Status != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatal("hash job timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func hashTrack(t *testing.T, c *client, id string) library.Track {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	if code != 200 {
		t.Fatalf("read tracks: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "SourceTracks", raw)
	var page library.Page
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	for _, track := range page.Items {
		if track.ID == id {
			return track
		}
	}
	t.Fatal("hash source missing")
	return library.Track{}
}

func TestYAMLHashJobsOriginalBytesCancelAndPlayback(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	for name, content := range map[string][]byte{"original.wav": payload, "empty.wav": {}, "slow.wav": make([]byte, 16<<20)} {
		if err := os.WriteFile(filepath.Join(music, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Hashing.BytesPerSecond = 1 << 20
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true,
		Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "music", Name: "Hash fixtures", Path: music}}}}}}}
	disableLocalLyricsImport(&cfg)
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	tracks := scanNCM(t, c)
	if len(tracks) != 3 {
		t.Fatalf("expected three hash fixtures, got %d", len(tracks))
	}
	for _, track := range tracks {
		if track.Asset == nil || track.Asset.HashStatus != "unknown" || track.Asset.FileSHA512 != nil {
			t.Fatal("automatic hashing was enabled by default")
		}
	}
	for _, name := range []string{"original.wav", "empty.wav"} {
		track := tracks[name]
		id := submitHash(t, c, track.ID, "hash-"+name)
		job := waitHash(t, c, id)
		content := payload
		if name == "empty.wav" {
			content = nil
		}
		digest := sha512.Sum512(content)
		current := hashTrack(t, c, track.ID)
		if job.Status != "succeeded" || job.Progress.CompletedBytes != int64(len(content)) || job.Progress.TotalBytes != int64(len(content)) ||
			current.Asset.HashStatus != "verified" || current.Asset.FileSHA512 == nil || *current.Asset.FileSHA512 != hex.EncodeToString(digest[:]) || current.Asset.HashComputedAt == nil {
			t.Fatalf("original file digest verification failed: %+v %+v", job, current.Asset)
		}
		if submitHash(t, c, track.ID, "hash-"+name) != id {
			t.Fatal("completed job idempotency did not replay")
		}
	}
	slowID := submitHash(t, c, tracks["slow.wav"].ID, "hash-slow-0001")
	for deadline := time.Now().Add(5 * time.Second); ; {
		job := readHash(t, c, slowID)
		if job.Status == "running" {
			break
		}
		if job.Status != "queued" || time.Now().After(deadline) {
			t.Fatalf("slow task did not start: %+v", job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	request := playback.CreateRequest{SourceTrackID: tracks["original.wav"].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav; codecs=\"1\"", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "hash-playback-0001")
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil {
		t.Fatalf("foreground playback failed during hashing: %d %s", code, raw)
	}
	verifyRanges(t, c, description, payload)
	t.Logf("Original playback and Range completed during background hashing in %s", time.Since(started))
	started = time.Now()
	code, raw, _ = c.request(t, "POST", "/api/v1/hash-jobs/"+slowID+"/cancel", struct{}{}, "")
	if code != 200 {
		t.Fatalf("running cancel: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "HashJob", raw)
	cancelled := readHash(t, c, slowID)
	if cancelled.Status != "cancelled" || cancelled.Error == nil || cancelled.Error.Code != "CANCELED" {
		t.Fatalf("task was not cancelled: %+v", cancelled)
	}
	t.Logf("Running hash cancellation completed in %s", time.Since(started))
	code, raw, _ = c.request(t, "POST", "/api/v1/hash-jobs/"+slowID+"/cancel", struct{}{}, "")
	var repeated identification.HashJob
	if code != 200 || json.Unmarshal(raw, &repeated) != nil || !cancelled.UpdatedAt.Equal(repeated.UpdatedAt) || repeated.Status != cancelled.Status {
		t.Fatal("repeat cancellation changed terminal state")
	}
	if current := hashTrack(t, c, tracks["slow.wav"].ID); current.Asset.FileSHA512 != nil || current.Asset.HashStatus == "verified" {
		t.Fatal("cancelled work published a complete digest")
	}
	changed := append([]byte(nil), payload...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(filepath.Join(music, "original.wav"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	updated := scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "hash-rescan-0001")["original.wav"]
	if updated.ID != tracks["original.wav"].ID || updated.AssetID == tracks["original.wav"].AssetID || updated.Asset.FileSHA512 != nil || updated.Asset.HashStatus != "unknown" {
		t.Fatal("same-size source replacement retained old verification")
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/source-tracks/"+updated.ID+"/hash-jobs", struct{}{}, "hash-original.wav")
	if code != 409 || !strings.Contains(string(raw), "CONFLICT") {
		t.Fatalf("changed source reused old hash request: %d %s", code, raw)
	}
	newJob := waitHash(t, c, submitHash(t, c, updated.ID, "hash-replaced-0001"))
	digest := sha512.Sum512(changed)
	current := hashTrack(t, c, updated.ID)
	if newJob.Status != "succeeded" || current.Asset.FileSHA512 == nil || *current.Asset.FileSHA512 != hex.EncodeToString(digest[:]) {
		t.Fatal("replacement asset did not receive its own complete digest")
	}
}

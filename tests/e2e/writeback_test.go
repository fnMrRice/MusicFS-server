package e2e_test

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestWriteBackActualServerPlaybackGateSelectionAndRestart(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual writeback validation")
	}
	provider, audio := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg")
	original, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	source := filepath.Join(music, "synthetic.flac")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	ungranted := t.TempDir()
	if err := os.WriteFile(filepath.Join(ungranted, "ungranted.flac"), original, 0600); err != nil {
		t.Fatal(err)
	}
	pcm := writeBackPCM(t, ffmpeg, original)
	if len(pcm) == 0 || bytes.Equal(pcm, make([]byte, len(pcm))) {
		t.Fatal("synthetic source did not decode to nonzero PCM")
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Synthetic writeback", Path: music}, {ID: "read-only-policy", Name: "No write grant", Path: ungranted}}}}}},
		{PluginID: "musicfs.audio.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "audio-default", Role: "audio", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	var target mediastore.ContentTarget
	var request writebackhost.Request
	var finalJob writebackhost.Job
	var ungrantedTrack library.Track
	objects := map[string]string{}
	var written []byte
	readSource := func(t *testing.T) []byte {
		t.Helper()
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Run("disabled-preserves-source", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		tracks := scanNCM(t, c)
		track := tracks["synthetic.flac"]
		ungrantedTrack = tracks["ungranted.flac"]
		if track.ID == "" {
			t.Fatal("missing synthetic source")
		}
		target = mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}
		var cover bytes.Buffer
		if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
			t.Fatal(err)
		}
		lyrics := uploadActualMedia(t, c, target, "lyrics", "und", "text/plain", []byte("Synthetic writeback lyrics\n"), "writeback-lyrics-upload")
		art := uploadActualMedia(t, c, target, "artwork", "", "image/png", cover.Bytes(), "writeback-cover-upload")
		for i, object := range []mediastore.ContentObject{lyrics, art} {
			input := map[string]any{"objectId": object.ID, "assetId": target.AssetID, "assetVersion": target.AssetVersion, "expectedRevision": i}
			code, raw, _ := c.request(t, "PUT", "/api/v1/source-tracks/"+track.ID+"/media-selection", input, "writeback-select-"+object.ID)
			if code != 200 {
				t.Fatalf("select: %d %s", code, raw)
			}
			testkit.ValidateHTTP(t, "MediaSelection", raw)
			objects[object.Metadata.Field] = object.ID
		}
		request = writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, ExpectedRevision: 2, Fields: []writebackhost.Field{{Name: "lyrics", Language: "und"}}}
		code, raw, _ := c.request(t, "POST", "/api/v1/writeback-jobs", request, "writeback-disabled-key")
		if code != 403 {
			t.Fatalf("disabled write: %d %s", code, raw)
		}
		if !bytes.Equal(readSource(t), original) {
			t.Fatal("disabled writeback changed source")
		}
		cap := writeBackCapability(t, c)
		if cap.Enabled {
			t.Fatal("default root implicitly authorized writeback")
		}
	})
	if target.SourceTrackID == "" {
		t.Fatal("initial phase failed")
	}
	cfg.WriteBack = config.WriteBackConfig{Enabled: true, MaxFileBytes: 8 << 30, Roots: []config.WriteBackRoot{{InstanceID: "local", RootID: "local", Fields: []string{"lyrics", "artwork"}}}}
	t.Run("explicit-grant-and-playback-gate", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		cap := writeBackCapability(t, c)
		if !cap.Enabled {
			t.Fatal("explicit root grant not exposed")
		}
		if runtime.GOOS != "windows" {
			code, raw, _ := c.request(t, "POST", "/api/v1/writeback-jobs", request, "writeback-unsupported-key")
			if code != 415 || cap.Available || !bytes.Equal(readSource(t), original) {
				t.Fatalf("unsupported platform: %d %s %+v", code, raw, cap)
			}
			return
		}
		if !cap.Available || cap.Replacement != "recoverableReplace" || len(cap.Formats) != 1 || cap.Formats[0] != "flac" {
			t.Fatalf("actual NTFS capability unavailable: %+v", cap)
		}
		for _, tc := range []struct {
			name   string
			input  writebackhost.Request
			status int
		}{
			{"stale-revision", func() writebackhost.Request { v := request; v.ExpectedRevision = 0; return v }(), 409},
			{"stale-asset", func() writebackhost.Request { v := request; v.AssetVersion = "stale"; return v }(), 409},
			{"overwrite-denied", func() writebackhost.Request { v := request; v.Overwrite = true; return v }(), 403},
			{"ungranted-root", func() writebackhost.Request {
				v := request
				v.SourceTrackID, v.AssetID, v.AssetVersion = ungrantedTrack.ID, ungrantedTrack.AssetID, ungrantedTrack.Asset.Version
				return v
			}(), 403},
		} {
			code, raw, _ := c.request(t, "POST", "/api/v1/writeback-jobs", tc.input, "writeback-"+tc.name)
			if code != tc.status {
				t.Fatalf("%s: %d %s", tc.name, code, raw)
			}
		}
		play := playback.CreateRequest{SourceTrackID: target.SourceTrackID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
		code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", play, "writeback-playback-before")
		var session playback.Description
		if code != 201 || json.Unmarshal(raw, &session) != nil {
			t.Fatalf("playback: %d %s", code, raw)
		}
		assertWriteBackPlayback(t, c, session, ffmpeg)
		cancelID := submitWriteBack(t, c, request, "writeback-cancel-before-write")
		code, raw, _ = c.request(t, "POST", "/api/v1/writeback-jobs/"+cancelID+"/cancel", map[string]any{}, "")
		if code != 200 {
			t.Fatalf("cancel queued: %d %s", code, raw)
		}
		jobID := submitWriteBack(t, c, request, "writeback-source-lyrics")
		time.Sleep(650 * time.Millisecond)
		queued := getWriteBack(t, c, jobID)
		if queued.Status != "queued" || !bytes.Equal(readSource(t), original) {
			t.Fatalf("active playback did not protect source: %+v", queued)
		}
		if code, raw, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+session.SessionID, nil, ""); code != 204 {
			t.Fatalf("stop: %d %s", code, raw)
		}
		for deadline := time.Now().Add(30 * time.Second); ; {
			finalJob = getWriteBack(t, c, jobID)
			if finalJob.Status != "queued" && finalJob.Status != "running" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("writeback timeout: %+v", finalJob)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if finalJob.Status != "succeeded" || finalJob.Result == nil || !finalJob.Result.Changed {
			t.Fatalf("writeback: %+v", finalJob)
		}
		written = readSource(t)
		digest := sha512.Sum512(written)
		if bytes.Equal(written, original) || finalJob.Result.FileSHA512 != hex.EncodeToString(digest[:]) {
			t.Fatal("source digest did not match writeback result")
		}
		if !bytes.Equal(writeBackPCM(t, ffmpeg, written), pcm) {
			t.Fatal("writeback modified decoded audio PCM")
		}
		if !bytes.Contains(written, []byte("Synthetic writeback lyrics")) {
			t.Fatal("selected lyrics not present in rewritten FLAC")
		}
		if finalJob.Result.PreviousAssetID != target.AssetID || finalJob.Result.AssetID == target.AssetID || finalJob.Result.AssetVersion == target.AssetVersion {
			t.Fatal("writeback did not create a new asset version")
		}
		if replay := submitWriteBack(t, c, request, "writeback-source-lyrics"); replay != jobID {
			t.Fatal("old request replay returned a new operation")
		}
		code, raw, _ = c.request(t, "POST", "/api/v1/writeback-jobs/"+jobID+"/recover", map[string]any{}, "")
		if code != 200 {
			t.Fatalf("completed recovery: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "WriteBackJob", raw)
		assertWriteBackSelection(t, c, target.SourceTrackID, finalJob, objects)
		code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", play, "writeback-playback-after")
		if code != 201 || json.Unmarshal(raw, &session) != nil {
			t.Fatalf("new playback: %d %s", code, raw)
		}
		assertWriteBackPlayback(t, c, session, ffmpeg)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+session.SessionID, nil, ""); code != 204 {
			t.Fatal("new playback stop failed")
		}
	})
	if runtime.GOOS != "windows" {
		return
	}
	if finalJob.Status != "succeeded" {
		t.Fatal("writeback phase failed")
	}
	cfg.WriteBack.Enabled = false
	t.Run("disabled-restart-and-rescan-keeps-identity", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		track := scanNCM(t, c)["synthetic.flac"]
		if track.ID != target.SourceTrackID || track.AssetID != finalJob.Result.AssetID || track.Asset.Version != finalJob.Result.AssetVersion {
			t.Fatalf("rescan changed source identity or asset: %+v", track)
		}
		if track.Asset.HashStatus != "verified" || track.Asset.FileSHA512 == nil || *track.Asset.FileSHA512 != finalJob.Result.FileSHA512 {
			t.Fatal("rescan lost the verified writeback file digest")
		}
		assertWriteBackSelection(t, c, target.SourceTrackID, finalJob, objects)
		if job := getWriteBack(t, c, finalJob.ID); job.Status != "succeeded" {
			t.Fatal("disabled policy hid committed job")
		}
		updated := request
		updated.AssetID = track.AssetID
		updated.AssetVersion = track.Asset.Version
		updated.ExpectedRevision = 3
		code, raw, _ := c.request(t, "POST", "/api/v1/writeback-jobs", updated, "writeback-disabled-restart")
		if code != 403 || !bytes.Equal(readSource(t), written) {
			t.Fatalf("disabled restart changed source: %d %s", code, raw)
		}
		untouched, err := os.ReadFile(filepath.Join(ungranted, "ungranted.flac"))
		if err != nil || !bytes.Equal(untouched, original) {
			t.Fatal("ungranted source changed")
		}
	})
}

func submitWriteBack(t *testing.T, c *client, input writebackhost.Request, key string) string {
	t.Helper()
	code, raw, _ := c.request(t, "POST", "/api/v1/writeback-jobs", input, key)
	var out struct {
		JobID string `json:"jobId"`
	}
	if code != 202 || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("writeback submit: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "WriteBackAccepted", raw)
	return out.JobID
}
func getWriteBack(t *testing.T, c *client, id string) writebackhost.Job {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/writeback-jobs/"+id, nil, "")
	var out writebackhost.Job
	if code != 200 || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("writeback get: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "WriteBackJob", raw)
	return out
}
func writeBackCapability(t *testing.T, c *client) library.RootWriteBack {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var page struct {
		Items []library.Provider `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &page) != nil {
		t.Fatalf("provider: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "Providers", raw)
	for _, p := range page.Items {
		if p.ID == "local" {
			for _, r := range p.Roots {
				if r.ID == "local" && r.WriteBack != nil {
					return *r.WriteBack
				}
			}
		}
	}
	t.Fatal("missing root writeback capability")
	return library.RootWriteBack{}
}
func assertWriteBackSelection(t *testing.T, c *client, track string, job writebackhost.Job, objects map[string]string) {
	t.Helper()
	selection := metadataSelection(t, c, track)
	if selection.Revision != 3 || selection.Target.AssetID != job.Result.AssetID || len(selection.Bindings) != 2 {
		t.Fatalf("selection not migrated: %+v", selection)
	}
	for _, b := range selection.Bindings {
		if !b.ManualLocked || !b.Current || b.State != "available" || b.ObjectID != objects[b.Field] || b.Target.AssetID != job.Result.AssetID {
			t.Fatalf("selection lock/object not preserved: %+v", b)
		}
	}
}
func writeBackPCM(t *testing.T, ffmpeg string, source []byte) []byte {
	t.Helper()
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-map", "0:a:0", "-f", "s16le", "pipe:1")
	cmd.Stdin = bytes.NewReader(source)
	var errors bytes.Buffer
	cmd.Stderr = &errors
	pcm, err := cmd.Output()
	if err != nil {
		t.Fatalf("decode source: %v %s", err, errors.String())
	}
	return pcm
}

func assertWriteBackPlayback(t *testing.T, c *client, session playback.Description, ffmpeg string) {
	t.Helper()
	if session.DeliveryMode != "processed" || session.OutputProfile.MIME != "audio/mpeg" {
		t.Fatal("FLAC did not use the existing MP3 playback profile")
	}
	code, raw, headers := c.request(t, "GET", session.StreamURL, nil, "")
	if code != 200 || headers.Get("Content-Type") != "audio/mpeg" || len(raw) < 100 {
		t.Fatalf("playback delivery: %d (%d bytes)", code, len(raw))
	}
	pcm := writeBackPCM(t, ffmpeg, raw)
	if len(pcm) == 0 || bytes.Equal(pcm, make([]byte, len(pcm))) {
		t.Fatal("playback did not decode to nonzero PCM")
	}
}

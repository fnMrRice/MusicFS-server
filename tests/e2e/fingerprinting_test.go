package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

// All samples are generated here, not extracted from commercially released music.
func fingerprintWave(seconds int) []byte {
	b := make([]byte, 44+seconds*48000*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 96000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	for i := 0; i < seconds*48000; i++ {
		at := float64(i) / 48000
		step := int(at * 4)
		hz := 220 * math.Pow(2, float64((step*7+step/8)%24)/12)
		x := .18*(.7+.3*math.Sin(2*math.Pi*3*at))*math.Sin(2*math.Pi*hz*at) + .09*math.Sin(2*math.Pi*(110+float64(step%7)*31)*at)
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(x*32767)))
	}
	return b
}

func TestYAMLFingerprintsActualToolsMediaAuthorizationAndPlayback(t *testing.T) {
	ffmpeg, ffprobe, fpcalc := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE"), os.Getenv("MUSICFS_FPCALC")
	if ffmpeg == "" || ffprobe == "" || fpcalc == "" {
		t.Skip("set MUSICFS_FFMPEG, MUSICFS_FFPROBE and MUSICFS_FPCALC for actual fingerprint E2E")
	}
	fpbytes, err := os.ReadFile(fpcalc)
	if err != nil {
		t.Fatal(err)
	}
	pin := fmt.Sprintf("%x", sha256.Sum256(fpbytes))
	provider, audio, ncm := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg"), build(t, "musicfs-plugin-ncm")
	music := t.TempDir()
	wave := fingerprintWave(32)
	payloads := map[string][]byte{"source.wav": wave}
	for _, format := range []string{"mp3", "flac"} {
		codec := "flac"
		if format == "mp3" {
			codec = "libmp3lame"
		}
		cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-c:a", codec, "-f", format, "pipe:1")
		cmd.Stdin = bytes.NewReader(wave)
		payloads["source."+format], err = cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
	}
	payloads["source.ncm"], err = testkit.BuildNCM(payloads["source.mp3"], map[string]any{"musicName": "Synthetic fingerprint source"})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range payloads {
		if err := os.WriteFile(filepath.Join(music, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	configuration := func(t *testing.T) (config.Config, string) {
		cfg, base := yamlServiceConfig(t)
		cfg.Plugins = []config.Plugin{
			{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Synthetic audio", Path: music}}}}}},
			{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
			{PluginID: "musicfs.plugin.decoder.ncm", Executable: ncm, Enabled: true, Instances: []config.Instance{{InstanceID: "96412f52-9ff4-5a82-9eaa-7644a5837cda", Enabled: true}}},
		}
		cfg.Fingerprinting = config.FingerprintingConfig{Enabled: true, FpcalcPath: fpcalc, FpcalcSHA256: pin, MaxConcurrent: 1, TimeoutSeconds: 30}
		disableLocalLyricsImport(&cfg)
		return cfg, base
	}
	t.Run("enabled", func(t *testing.T) {
		cfg, base := configuration(t)
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		tracks := scanNCM(t, c)
		if len(tracks) != 4 {
			t.Fatalf("expected 4 sources, got %d", len(tracks))
		}
		create := func(name, key string, start, duration int64) string {
			t.Helper()
			code, raw, _ := c.request(t, "POST", "/api/v1/source-tracks/"+tracks[name].ID+"/fingerprint-jobs", map[string]int64{"startMs": start, "durationMs": duration}, key)
			var accepted struct {
				JobID string `json:"jobId"`
			}
			if code != 202 || json.Unmarshal(raw, &accepted) != nil {
				t.Fatalf("create %s: %d %s", name, code, raw)
			}
			testkit.ValidateHTTP(t, "FingerprintAccepted", raw)
			return accepted.JobID
		}
		results := map[string]identification.FingerprintIndex{}
		for _, name := range []string{"source.mp3", "source.flac", "source.ncm"} {
			id := create(name, "fingerprint-"+name, 0, 120000)
			job := waitFingerprint(t, c, id)
			if job.Status != "succeeded" || job.Result == nil {
				t.Fatalf("fingerprint %s: %+v", name, job)
			}
			index := *job.Result
			results[name] = index
			if index.Quality.Status != "available" || index.ValueCount < 100 || !index.Current || index.ExtractorIdentity.Analyzer.SHA256 != pin {
				t.Fatalf("invalid real fingerprint: %+v", index)
			}
			if index.SampleCount < 31*11025 || index.SampleCount > 33*11025 {
				t.Fatalf("false coverage: %+v", index)
			}
			code, raw, _ := c.request(t, "GET", "/api/v1/fingerprints/"+index.ID, nil, "")
			if code != 200 {
				t.Fatalf("detail: %d %s", code, raw)
			}
			testkit.ValidateHTTP(t, "FingerprintDetail", raw)
			var detail fingerprint.Detail
			if json.Unmarshal(raw, &detail) != nil || detail.Identity != index.ExtractorIdentity || len(detail.Values) != index.ValueCount || detail.PCM.SampleCount != index.SampleCount {
				t.Fatal("detail/index mismatch")
			}
			if _, err := detail.Encode(); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(filepath.Join(cfg.MediaRoot, "objects", index.Object.ID))
			if err != nil || int64(len(stored)) != index.Object.Size || fmt.Sprintf("%x", sha256.Sum256(stored)) != index.Object.SHA256 {
				t.Fatal("immutable media bytes do not match index", err)
			}
			if bytes.Contains(raw, []byte(music)) || bytes.Contains(raw, []byte("relativePath")) {
				t.Fatal("private path exposed")
			}
			response, err := http.Get(base + "/api/v1/fingerprints/" + index.ID)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 401 {
				t.Fatal("anonymous detail read accepted")
			}
			t.Logf("%s: %d samples, %d values, %s", name, index.SampleCount, index.ValueCount, index.Quality.Status)
		}
		// Lossless input proves floor-quantized range boundaries independent of codec padding.
		ranged := waitFingerprint(t, c, create("source.flac", "fingerprint-range", 7, 10000))
		if ranged.Result == nil || ranged.Result.ActualStartSample != 77 || ranged.Result.SampleCount != 110250 {
			t.Fatalf("precise range missing: %+v", ranged)
		}
		short := waitFingerprint(t, c, create("source.flac", "fingerprint-short", 30000, 10000))
		if short.Result == nil || short.Result.Quality.Status != "short" || short.Result.SampleCount != 22050 {
			t.Fatalf("tail expanded: %+v", short)
		}
		track := tracks["source.flac"].ID
		var page struct {
			Items     []identification.FingerprintIndex `json:"items"`
			NextAfter *string                           `json:"nextAfter"`
		}
		code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks/"+track+"/fingerprints?limit=1", nil, "")
		if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 1 || page.NextAfter == nil {
			t.Fatalf("first page: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "FingerprintIndexes", raw)
		first := page.Items[0].ID
		code, raw, _ = c.request(t, "GET", "/api/v1/source-tracks/"+track+"/fingerprints?limit=1&after="+*page.NextAfter, nil, "")
		if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 1 || page.Items[0].ID == first {
			t.Fatalf("second page: %d %s", code, raw)
		}
		cancelID := create("source.flac", "fingerprint-cancel", 123, 120000)
		code, raw, _ = c.request(t, "POST", "/api/v1/fingerprint-jobs/"+cancelID+"/cancel", struct{}{}, "")
		var cancelled identification.FingerprintJob
		if code != 200 || json.Unmarshal(raw, &cancelled) != nil || cancelled.Status != "cancelled" {
			t.Fatalf("cancel: %d %s", code, raw)
		}
		code, raw, _ = c.request(t, "POST", "/api/v1/fingerprint-jobs/"+cancelID+"/cancel", struct{}{}, "")
		var again identification.FingerprintJob
		if code != 200 || json.Unmarshal(raw, &again) != nil || again.Status != "cancelled" || !again.UpdatedAt.Equal(cancelled.UpdatedAt) {
			t.Fatal("repeat cancel changed result")
		}
		verifyFingerprintPlayback(t, c, tracks["source.wav"].ID, wave)
		// Database-backed indexes remain available when the original source disappears.
		if err := os.Remove(filepath.Join(music, "source.flac")); err != nil {
			t.Fatal(err)
		}
		code, raw, _ = c.request(t, "GET", "/api/v1/source-tracks/"+track+"/fingerprints", nil, "")
		if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 3 {
			t.Fatalf("offline index query: %d %s", code, raw)
		}
		code, _, _ = c.request(t, "GET", "/api/v1/fingerprints/"+results["source.flac"].ID, nil, "")
		if code != 200 {
			t.Fatal("offline detail unavailable")
		}
	})
	for _, mode := range []string{"disabled", "wrong-tool-checksum"} {
		t.Run(mode, func(t *testing.T) {
			cfg, base := configuration(t)
			if mode == "disabled" {
				cfg.Fingerprinting.Enabled = false
			} else {
				cfg.Fingerprinting.FpcalcSHA256 = strings.Repeat("0", 64)
			}
			startNCMServer(t, cfg, base)
			c := login(t, base, cfg.AdminToken)
			tracks := scanNCM(t, c)
			code, raw, _ := c.request(t, "POST", "/api/v1/source-tracks/"+tracks["source.wav"].ID+"/fingerprint-jobs", map[string]int64{"startMs": 0, "durationMs": 10000}, "fingerprint-disabled")
			if code != 415 || !bytes.Contains(raw, []byte("UNSUPPORTED")) {
				t.Fatalf("capability fallback: %d %s", code, raw)
			}
			verifyFingerprintPlayback(t, c, tracks["source.wav"].ID, wave)
		})
	}
}

func waitFingerprint(t *testing.T, c *client, id string) identification.FingerprintJob {
	t.Helper()
	for deadline := time.Now().Add(40 * time.Second); ; {
		code, raw, _ := c.request(t, "GET", "/api/v1/fingerprint-jobs/"+id, nil, "")
		var job identification.FingerprintJob
		if code != 200 || json.Unmarshal(raw, &job) != nil {
			t.Fatalf("job: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "FingerprintJob", raw)
		if job.Status != "queued" && job.Status != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("fingerprint deadline: %s", raw)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func verifyFingerprintPlayback(t *testing.T, c *client, track string, wave []byte) {
	t.Helper()
	request := playback.CreateRequest{SourceTrackID: track, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "fingerprint-playback-regression")
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil {
		t.Fatalf("playback: %d %s", code, raw)
	}
	verifyRanges(t, c, description, wave)
	if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
		t.Fatal("stop failed")
	}
}

package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMetadataActualServerCandidatesApplyCancelAndRestart(t *testing.T) {
	provider, scraper, decoder := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-scraper-local"), build(t, "musicfs-plugin-ncm")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	lyrics := []byte("[00:01.00]测试歌词\n[00:02.00]Synthetic text")
	tag := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: append([]byte{3, 'z', 'h', 'o', 0}, lyrics...)}, testkit.ScraperFrame{ID: "APIC", Body: append([]byte("\x03image/png\x00\x03\x00"), testkit.ScraperPNG()...)})
	tagged := append(tag, payload...)
	encrypted, err := testkit.BuildNCM(tagged, nil)
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	samples := map[string][]byte{"tagged.mp3": tagged, "tagged.lrc": []byte("[00:01.00]Synthetic sidecar"), "plain.wav": wave, "gone.wav": wave, "plain.lrc": {0xff, 0xfe, 0xff}, "cover.png": testkit.ScraperPNG(), "sealed.ncm": encrypted, "invalid.ncm": []byte("CTENFDAMinvalid")}
	for name, body := range samples {
		if err := os.WriteFile(filepath.Join(music, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	importArtwork := false
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, ImportDefaults: &pluginproto.ImportOverrides{ImportArtwork: &importArtwork, ImportLyrics: &importArtwork}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Metadata fixture", Path: music}}}}}},
		{PluginID: "musicfs.scraper.local", Executable: scraper, Enabled: true, Instances: []config.Instance{{InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", Name: "Local metadata", Enabled: true}, {InstanceID: "4d8409cd-1d37-5e46-8e79-1b8b48e78020", Enabled: false}}},
		{PluginID: "musicfs.plugin.decoder.ncm", Executable: decoder, Enabled: true, Instances: []config.Instance{{InstanceID: "96412f52-9ff4-5a82-9eaa-7644a5837cda", Enabled: true}}},
	}
	var trackID, jobID string
	completed := false
	var selected enrichment.PreviewCandidate
	t.Run("search-preview-apply-cancel", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		tracks := scanNCM(t, c)
		trackID = tracks["tagged.mp3"].ID
		if err := os.Remove(filepath.Join(music, "gone.wav")); err != nil {
			t.Fatal(err)
		}
		code, raw, _ := c.request(t, "GET", "/api/v1/scraper-instances", nil, "")
		var instances struct {
			Items []enrichment.Instance `json:"items"`
		}
		if code != 200 || json.Unmarshal(raw, &instances) != nil || len(instances.Items) != 2 {
			t.Fatalf("scraper instances: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "ScraperInstances", raw)
		for _, instance := range instances.Items {
			if instance.QueryCorrectionsSupported || (instance.InstanceID == "4d8409cd-1d37-5e46-8e79-1b8b48e78020" && instance.Availability != "offline") {
				t.Fatal("unsafe scraper capability declaration")
			}
		}
		for name, tc := range map[string]struct {
			input map[string]any
			code  int
		}{
			"duplicate":        {metadataRequest([]string{trackID, trackID}, "lyrics"), 400},
			"missing":          {metadataRequest([]string{"absent-source"}, "lyrics"), 404},
			"query":            {map[string]any{"sourceTrackIds": []string{trackID}, "fields": []string{"lyrics"}, "query": map[string]any{"title": "Different title"}}, 415},
			"null-languages":   {map[string]any{"sourceTrackIds": []string{trackID}, "fields": []string{"lyrics"}, "languages": nil}, 400},
			"offline":          {map[string]any{"sourceTrackIds": []string{trackID}, "fields": []string{"lyrics"}, "instanceIds": []string{"4d8409cd-1d37-5e46-8e79-1b8b48e78020"}}, 503},
			"unknown-instance": {map[string]any{"sourceTrackIds": []string{trackID}, "fields": []string{"lyrics"}, "instanceIds": []string{"a1f02476-a49b-58ad-9218-af65aace0ffe"}}, 404},
			"exclude-all":      {map[string]any{"sourceTrackIds": []string{trackID}, "fields": []string{"lyrics"}, "excludeInstanceIds": []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}}, 415},
			"fake-scope":       {map[string]any{"sourceTrackIds": []string{trackID}, "fields": []string{"lyrics"}, "resources": []any{}}, 400},
		} {
			code, body, _ := c.request(t, "POST", "/api/v1/metadata-jobs", tc.input, "metadata-invalid-"+name)
			if code != tc.code {
				t.Fatalf("%s: %d %s", name, code, body)
			}
			testkit.ValidateHTTP(t, "Error", body)
		}
		for _, check := range []struct {
			header, value string
			status        int
		}{{"X-CSRF-Token", "", 403}, {"Origin", "https://untrusted.invalid", 403}} {
			body, _ := json.Marshal(metadataRequest([]string{trackID}, "lyrics"))
			r, _ := http.NewRequest("POST", base+"/api/v1/metadata-jobs", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", base)
			r.Header.Set("X-CSRF-Token", c.csrf)
			r.Header.Set("Idempotency-Key", "metadata-auth-test")
			r.Header.Set(check.header, check.value)
			resp, err := c.http.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != check.status {
				t.Fatal("metadata request bypassed mutation authorization")
			}
		}
		anonymous, err := http.Get(base + "/api/v1/scraper-instances")
		if err != nil {
			t.Fatal(err)
		}
		anonymous.Body.Close()
		if anonymous.StatusCode != 401 {
			t.Fatal("anonymous scraper list accepted")
		}
		input := metadataRequest([]string{trackID, tracks["plain.wav"].ID, tracks["invalid.ncm"].ID, tracks["gone.wav"].ID}, "lyrics")
		job := metadataJob(t, c, input, "metadata-batch-key")
		jobID = job.JobID
		if job.State != "completed" || len(job.Targets) != 4 {
			t.Fatalf("partial batch did not complete: %+v", job)
		}
		var sidecar enrichment.PreviewCandidate
		failed, unavailable := false, false
		for _, target := range job.Targets {
			if target.Target.SourceTrackID == tracks["gone.wav"].ID {
				failed = target.State == "failed" && target.FailureCode != nil
			}
			for _, candidate := range target.Candidates {
				if candidate.Status == "unavailable" {
					unavailable = true
					continue
				}
				if candidate.Target.SourceTrackID == trackID && candidate.Origin == "embedded" {
					selected = candidate
				}
				if candidate.Target.SourceTrackID == trackID && candidate.Origin == "sidecar" {
					sidecar = candidate
				}
			}
		}
		if !failed || !unavailable || selected.PreviewObjectID == nil || sidecar.PreviewObjectID == nil || selected.Language != "zh" || sidecar.Language != "und" {
			t.Fatalf("missing independent target/candidate results: %+v", job)
		}
		code, raw, _ = c.request(t, "GET", "/api/v1/media/"+*selected.PreviewObjectID, nil, "")
		if code != 200 || !bytes.Equal(raw, lyrics) {
			t.Fatal("preview bytes mismatch")
		}
		if selection := metadataSelection(t, c, trackID); selection.Revision != 0 || len(selection.Bindings) != 0 {
			t.Fatal("metadata search automatically selected a candidate")
		}
		if replay := metadataJob(t, c, input, "metadata-batch-key"); replay.JobID != jobID {
			t.Fatal("metadata job did not replay")
		}
		if selection := applyMetadata(t, c, selected.CandidateID, 0, "metadata-embedded-apply"); selection.Revision != 1 || !selection.Bindings[0].ManualLocked {
			t.Fatal("metadata selection did not lock")
		}
		code, _, _ = c.request(t, "POST", "/api/v1/metadata-candidates/"+sidecar.CandidateID+"/apply", map[string]any{"expectedRevision": 0}, "metadata-stale-apply")
		if code != 409 {
			t.Fatal("stale revision overwrote a choice")
		}
		applyMetadata(t, c, sidecar.CandidateID, 1, "metadata-sidecar-apply")
		artInput := metadataRequest([]string{trackID}, "artwork")
		artInput["languages"] = []string{"zh"}
		artJob := metadataJob(t, c, artInput, "metadata-artwork-key")
		var art enrichment.PreviewCandidate
		for _, candidate := range artJob.Targets[0].Candidates {
			if candidate.Origin == "embedded" {
				art = candidate
			}
		}
		if art.PreviewObjectID == nil || art.Language != "" {
			t.Fatal("artwork preview language is not empty")
		}
		if selection := applyMetadata(t, c, art.CandidateID, 2, "metadata-artwork-apply"); selection.Revision != 3 || len(selection.Bindings) != 3 {
			t.Fatal("sequential field selection failed")
		}
		code, raw, _ = c.request(t, "POST", "/api/v1/metadata-jobs/"+jobID+"/cancel", struct{}{}, "")
		if code != 200 {
			t.Fatalf("cancel: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "MetadataJob", raw)
		code, _, _ = c.request(t, "POST", "/api/v1/metadata-candidates/"+sidecar.CandidateID+"/apply", map[string]any{"expectedRevision": 3}, "metadata-cancelled-apply")
		if code != 409 {
			t.Fatal("cancelled candidate applied")
		}
		if replay := applyMetadata(t, c, selected.CandidateID, 0, "metadata-embedded-apply"); replay.Revision != 1 {
			t.Fatal("successful apply lost idempotency after cancellation")
		}
		languageInput := metadataRequest([]string{trackID}, "lyrics")
		languageInput["languages"] = []string{"en"}
		languageJob := metadataJob(t, c, languageInput, "metadata-language-key")
		if len(languageJob.Targets[0].Candidates) != 0 || len(languageJob.Targets[0].Issues) == 0 {
			t.Fatal("unknown language was falsely matched")
		}
		ncmJob := metadataJob(t, c, metadataRequest([]string{tracks["sealed.ncm"].ID}, "lyrics"), "metadata-ncm-key")
		if ncmJob.State != "completed" || len(ncmJob.Targets[0].Candidates) == 0 || ncmJob.Targets[0].Candidates[0].PreviewObjectID == nil {
			t.Fatalf("NCM metadata unavailable: %+v", ncmJob)
		}
		play := playback.CreateRequest{SourceTrackID: tracks["plain.wav"].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
		code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", play, "metadata-playback-key")
		var description playback.Description
		if code != 201 || json.Unmarshal(raw, &description) != nil {
			t.Fatalf("playback: %d %s", code, raw)
		}
		verifyRanges(t, c, description, wave)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
			t.Fatal("stop failed")
		}
		for name, body := range samples {
			if name == "gone.wav" {
				continue
			}
			actual, err := os.ReadFile(filepath.Join(music, name))
			if err != nil || sha256.Sum256(actual) != sha256.Sum256(body) {
				t.Fatal("metadata changed source or sidecar content")
			}
		}
		completed = true
	})
	t.Run("disabled-restart-retains-content", func(t *testing.T) {
		if !completed {
			t.Skip("initial phase failed")
		}
		cfg.Plugins[1].Enabled = false
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		selection := metadataSelection(t, c, trackID)
		if selection.Revision != 3 || len(selection.Bindings) != 3 {
			t.Fatal("selection did not survive disabled restart")
		}
		for _, binding := range selection.Bindings {
			if !binding.ManualLocked {
				t.Fatal("manual lock lost on restart")
			}
		}
		code, raw, _ := c.request(t, "GET", "/api/v1/media/"+*selected.PreviewObjectID, nil, "")
		if code != 200 || !bytes.Equal(raw, lyrics) {
			t.Fatal("disabled scraper removed published content")
		}
		code, raw, _ = c.request(t, "GET", "/api/v1/metadata-jobs/"+jobID, nil, "")
		if code != 200 {
			t.Fatal("stored metadata job unavailable")
		}
		testkit.ValidateHTTP(t, "MetadataJob", raw)
		code, _, _ = c.request(t, "POST", "/api/v1/metadata-jobs", metadataRequest([]string{trackID}, "lyrics"), "metadata-disabled-key")
		if code != 503 {
			t.Fatal("disabled scraper accepted a new task")
		}
	})
}

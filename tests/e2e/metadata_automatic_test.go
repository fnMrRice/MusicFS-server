package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAutomaticMetadataActualServerImportDepthLocksAndRestart(t *testing.T) {
	provider, scraper := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-scraper-local")
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	mp3, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	tag := testkit.ScraperID3(4,
		testkit.ScraperFrame{ID: "USLT", Body: append([]byte{3, 'z', 'h', 'o', 0}, []byte("Synthetic embedded lyrics")...)},
		testkit.ScraperFrame{ID: "APIC", Body: append([]byte("\x03image/png\x00\x03\x00"), testkit.ScraperPNG()...)})
	music := t.TempDir()
	samples := map[string][]byte{
		"root.wav": wave, "root.lrc": []byte("[00:01.00]Original root lyrics"),
		"sub/child.wav": wave, "sub/child.lrc": []byte("[00:01.00]Child lyrics"),
		"sub/deep/deep.wav": wave, "sub/deep/deep.lrc": []byte("[00:01.00]Deep lyrics"),
		"tagged.mp3": append(tag, mp3...), "cover.png": testkit.ScraperPNG(),
	}
	for name, body := range samples {
		path := filepath.Join(music, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Automatic fixtures", Path: music}}}}}},
		{PluginID: "musicfs.scraper.local", Executable: scraper, Enabled: true, Instances: []config.Instance{{InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", Enabled: true}}},
	}
	cfg.Metadata.Automatic = enrichment.AutomaticPolicy{InstancePriority: []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}, Fields: []string{"lyrics", "artwork"}, Languages: []string{"en"}}
	var rootID, taggedID, lockedObject string
	var savedScan string
	completed := false
	t.Run("explicit-import-independent-of-global-policy", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		scan, tracks := automaticScan(t, c, map[string]any{"recursive": false, "importLyrics": false}, "automatic-disabled-scan")
		results := waitAutomatic(t, c, scan, 2)
		if len(tracks) != 2 {
			t.Fatalf("root-only scan: %d", len(tracks))
		}
		rootID, taggedID = tracks["root.wav"].ID, tracks["tagged.mp3"].ID
		for _, result := range results {
			if result.Reason != "automatic" || result.State != "skipped" || result.JobID != nil {
				t.Fatalf("disabled policy ran: %+v", result)
			}
		}
		if selection := metadataSelection(t, c, rootID); len(selection.Bindings) != 0 {
			t.Fatal("disabled import selected content")
		}
		scan, _ = automaticScan(t, c, map[string]any{"recursive": false, "importLyrics": true}, "automatic-import-root")
		results = waitAutomatic(t, c, scan, 4)
		for _, id := range []string{rootID, taggedID} {
			result := automaticResult(t, results, id, "importLyrics")
			if result.State != "completed" || len(result.Actions) != 1 || result.Actions[0].Status != "applied" {
				t.Fatalf("explicit lyrics import failed: %+v", result)
			}
			selection := metadataSelection(t, c, id)
			if selection.Revision != 1 || len(selection.Bindings) != 1 || selection.Bindings[0].ManualLocked || selection.Bindings[0].Field != "lyrics" {
				t.Fatal("automatic selection acquired manual lock or artwork")
			}
		}
		job := metadataJob(t, c, metadataRequest([]string{rootID}, "lyrics"), "automatic-manual-preview")
		if len(job.Targets) != 1 || len(job.Targets[0].Candidates) != 1 {
			t.Fatalf("manual preview: %+v", job)
		}
		selection := applyMetadata(t, c, job.Targets[0].Candidates[0].CandidateID, 1, "automatic-manual-lock")
		if selection.Revision != 2 || !selection.Bindings[0].ManualLocked {
			t.Fatal("manual lock not set")
		}
		lockedObject = selection.Bindings[0].ObjectID
		samples["root.lrc"] = []byte("[00:01.00]Changed candidate must not replace the manual choice")
		if err := os.WriteFile(filepath.Join(music, "root.lrc"), samples["root.lrc"], 0600); err != nil {
			t.Fatal(err)
		}
		scan, tracks = automaticScan(t, c, map[string]any{"recursive": true, "maxDepth": 1, "importLyrics": true}, "automatic-depth-one")
		results = waitAutomatic(t, c, scan, 6)
		if len(tracks) != 3 || tracks["deep.wav"].ID != "" {
			t.Fatal("lyrics import escaped scan depth")
		}
		rootResult := automaticResult(t, results, rootID, "importLyrics")
		if len(rootResult.Actions) != 1 || rootResult.Actions[0].Reason != "manualLocked" {
			t.Fatalf("manual lock ignored: %+v", rootResult)
		}
		if selection := metadataSelection(t, c, rootID); selection.Revision != 2 || selection.Bindings[0].ObjectID != lockedObject {
			t.Fatal("automatic import changed manual content")
		}
		if result := automaticResult(t, results, tracks["child.wav"].ID, "importLyrics"); len(result.Actions) != 1 || result.Actions[0].Status != "applied" {
			t.Fatalf("depth-one lyrics not imported: %+v", result)
		}
		completed = true
	})
	t.Run("global-policy-after-restart", func(t *testing.T) {
		if !completed {
			t.Skip("initial phase failed")
		}
		cfg.Metadata.Automatic.Enabled = true
		cfg.Metadata.Automatic.Languages = []string{}
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		scan, tracks := automaticScan(t, c, map[string]any{"importLyrics": false}, "automatic-global-restart")
		savedScan = scan
		results := waitAutomatic(t, c, scan, 4)
		if len(tracks) != 4 || tracks["root.wav"].ID != rootID || tracks["tagged.mp3"].ID != taggedID {
			t.Fatal("restart changed source identity")
		}
		deep := metadataSelection(t, c, tracks["deep.wav"].ID)
		if len(deep.Bindings) != 1 || deep.Bindings[0].Field != "lyrics" || deep.Bindings[0].ManualLocked {
			t.Fatal("global policy did not fill missing lyrics")
		}
		tagged := metadataSelection(t, c, taggedID)
		if len(tagged.Bindings) != 2 {
			t.Fatalf("embedded artwork not selected: %+v", automaticResult(t, results, taggedID, "automatic"))
		}
		for _, binding := range tagged.Bindings {
			if binding.ManualLocked {
				t.Fatal("automatic policy set a manual lock")
			}
		}
		root := metadataSelection(t, c, rootID)
		if root.Revision != 2 || len(root.Bindings) != 1 || root.Bindings[0].ObjectID != lockedObject || !root.Bindings[0].ManualLocked {
			t.Fatal("global policy replaced manual lyrics or guessed directory artwork")
		}
		code, body, _ := c.request(t, "GET", "/api/v1/media/"+lockedObject, nil, "")
		if code != 200 || !bytes.Equal(body, []byte("[00:01.00]Original root lyrics")) {
			t.Fatal("manual object changed on disk")
		}
	})
	t.Run("disabled-scraper-preserves-content-and-playback", func(t *testing.T) {
		if savedScan == "" {
			t.Skip("global phase failed")
		}
		cfg.Metadata.Automatic.Enabled = false
		cfg.Plugins[1].Enabled = false
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		if len(waitAutomatic(t, c, savedScan, 4)) != 4 {
			t.Fatal("results lost on restart")
		}
		if selection := metadataSelection(t, c, rootID); selection.Revision != 2 || selection.Bindings[0].ObjectID != lockedObject {
			t.Fatal("disabled scraper changed saved choice")
		}
		scan, _ := automaticScan(t, c, map[string]any{"recursive": false, "importLyrics": true}, "automatic-disabled-worker")
		for _, result := range waitAutomatic(t, c, scan, 4) {
			if result.Reason == "importLyrics" && (result.State != "failed" || result.FailureCode == nil || *result.FailureCode != "SOURCE_OFFLINE") {
				t.Fatalf("disabled scraper silently succeeded: %+v", result)
			}
		}
		play := playback.CreateRequest{SourceTrackID: rootID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
		code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", play, "automatic-playback-regression")
		var description playback.Description
		if code != 201 || json.Unmarshal(raw, &description) != nil {
			t.Fatalf("playback: %d %s", code, raw)
		}
		verifyRanges(t, c, description, wave)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
			t.Fatal("playback stop failed")
		}
	})
	for name, expected := range samples {
		actual, err := os.ReadFile(filepath.Join(music, filepath.FromSlash(name)))
		if err != nil || sha256.Sum256(actual) != sha256.Sum256(expected) {
			t.Fatalf("import modified source %s", name)
		}
	}
}

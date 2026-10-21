package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

func TestYAMLScanExclusionsRealWorkerAndSafeReconciliation(t *testing.T) {
	worker := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	for _, name := range []string{"public.wav", "private/hidden.wav", "sub/skip1.wav"} {
		path := filepath.Join(music, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	rules := []string{"private", "**/skip?.wav"}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: worker, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Name: "Local", Enabled: true, ImportDefaults: &pluginproto.ImportOverrides{ExcludePatterns: &rules}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "root", Name: "Music", Path: music}}}}}}}
	var before map[string]library.Track
	t.Run("scan", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		request := providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51")
		sequence := 0
		submit := func(options map[string]any) jobs.Job {
			sequence++
			request["options"] = options
			accepted := albumRequest[struct {
				JobID string `json:"jobId"`
			}](t, c, "POST", "/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, fmt.Sprintf("exclude-scan-%04d", sequence), 202, "ScanAccepted")
			return waitScanOptions(t, c, accepted.JobID)
		}
		list := func() map[string]library.Track {
			page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
			items := map[string]library.Track{}
			for _, item := range page.Items {
				items[item.DisplayName] = item
			}
			return items
		}
		job := submit(map[string]any{})
		if !reflect.DeepEqual(job.EffectiveOptions.ExcludePatterns, []string{"**/skip?.wav", "private"}) || len(list()) != 1 {
			t.Fatal("YAML exclusions did not govern host enumeration")
		}
		submit(map[string]any{"excludePatterns": []string{}})
		before = list()
		if len(before) != 3 {
			t.Fatal("explicit empty rules did not restore entire scope")
		}
		if err := os.Remove(filepath.Join(music, "private", "hidden.wav")); err != nil {
			t.Fatal(err)
		}
		submit(map[string]any{})
		after := list()
		for name, old := range before {
			if after[name].ID != old.ID || after[name].Availability != "available" {
				t.Fatalf("excluded absence damaged existing source: %s", name)
			}
		}
		request["options"] = map[string]any{"excludePatterns": []string{"private", "**/skip?.wav"}}
		first := albumRequest[struct {
			JobID string `json:"jobId"`
		}](t, c, "POST", "/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "exclude-order-key", 202, "ScanAccepted")
		request["options"] = map[string]any{"excludePatterns": []string{"**/skip?.wav", "private"}}
		second := albumRequest[struct {
			JobID string `json:"jobId"`
		}](t, c, "POST", "/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "exclude-order-key", 202, "ScanAccepted")
		if first.JobID != second.JobID {
			t.Fatal("same exclusion set failed idempotent replay")
		}
		request["options"] = map[string]any{"excludePatterns": []string{}}
		albumRequest[json.RawMessage](t, c, "POST", "/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "exclude-order-key", 409, "Error")
		waitScanOptions(t, c, first.JobID)
		for index, invalid := range []any{nil, "private", []string{"../private"}, []string{"a", "a"}, []string{"C:/private"}, []string{`a\b`}} {
			request["options"] = map[string]any{"excludePatterns": invalid}
			albumRequest[json.RawMessage](t, c, "POST", "/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, fmt.Sprintf("bad-exclude-%04d", index), 400, "Error")
		}
	})
	t.Run("restart-and-remove-exclusion", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		request := providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51")
		request["options"] = map[string]any{"excludePatterns": []string{}}
		accepted := albumRequest[struct {
			JobID string `json:"jobId"`
		}](t, c, "POST", "/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", request, "exclude-after-restart", 202, "ScanAccepted")
		waitScanOptions(t, c, accepted.JobID)
		page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
		for _, track := range page.Items {
			if track.ID != before[track.DisplayName].ID {
				t.Fatal("exclusion or restart changed stable source identity")
			}
			if track.DisplayName == "hidden.wav" && track.Availability != "missing" {
				t.Fatal("restored full scope failed to reconcile excluded disappearance")
			}
		}
	})
	for _, name := range []string{"public.wav", "sub/skip1.wav"} {
		actual, err := os.ReadFile(filepath.Join(music, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(actual, payload) {
			t.Fatal("scanning altered source bytes", err)
		}
	}
}

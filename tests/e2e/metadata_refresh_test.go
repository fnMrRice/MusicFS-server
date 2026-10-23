package e2e_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestDirectoryMetadataRefreshReplacesUnlockedContentWithinOneRoot(t *testing.T) {
	const providerID = "b1548e96-3081-5c9c-8376-bf290e369d51"
	mp3, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	first, second := t.TempDir(), t.TempDir()
	for index, root := range []string{first, second} {
		name := []string{"first", "second"}[index]
		if err := os.WriteFile(filepath.Join(root, name+".mp3"), mp3, 0600); err != nil {
			t.Fatal(err)
		}
		writeRefreshSidecars(t, root, name, 1)
	}
	disabled := false
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: providerID, Enabled: true, ImportDefaults: &pluginproto.ImportOverrides{ImportLyrics: &disabled, ImportArtwork: &disabled}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "one", Name: "First", Path: first}, {ID: "two", Name: "Second", Path: second}}}}}},
		{PluginID: "musicfs.scraper.local", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", Enabled: true}}},
	}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	scan, tracks := automaticScan(t, c, map[string]any{"importLyrics": true, "importArtwork": true}, "refresh-initial-import")
	waitAutomatic(t, c, scan, 6)
	firstID, secondID := tracks["first.mp3"].ID, tracks["second.mp3"].ID
	beforeFirst, beforeSecond := metadataSelection(t, c, firstID), metadataSelection(t, c, secondID)
	if len(beforeFirst.Bindings) != 2 || len(beforeSecond.Bindings) != 2 {
		t.Fatal("initial metadata was not imported")
	}
	writeRefreshSidecars(t, first, "first", 2)
	writeRefreshSidecars(t, second, "second", 2)
	refresh := func(key string) mediastore.Selection {
		t.Helper()
		request := providerScanRequest(t, c, providerID)
		request["rootIds"] = []string{"one"}
		request["options"] = map[string]any{"refreshMetadata": true}
		code, raw, _ := c.request(t, "POST", "/api/v1/provider-instances/"+providerID+"/scan-jobs", request, key)
		var accepted struct {
			JobID string `json:"jobId"`
		}
		if code != 202 || json.Unmarshal(raw, &accepted) != nil {
			t.Fatalf("refresh request failed: %d %s", code, raw)
		}
		job := waitScanOptions(t, c, accepted.JobID)
		if len(job.RootIDs) != 1 || job.RootIDs[0] != "one" || !job.EffectiveOptions.ImportLyrics || !job.EffectiveOptions.ImportArtwork {
			t.Fatalf("refresh plan changed scope or omitted metadata: %+v", job)
		}
		results := waitAutomatic(t, c, accepted.JobID, 1)
		if results[0].Reason != "refreshMetadata" || results[0].SourceTrackID != firstID || results[0].State != "completed" {
			t.Fatalf("refresh result mismatch: %+v", results)
		}
		return metadataSelection(t, c, firstID)
	}
	after := refresh("refresh-first-directory")
	for _, field := range []string{"lyrics", "artwork"} {
		previous, current := refreshBinding(t, beforeFirst, field), refreshBinding(t, after, field)
		if previous.ObjectID == current.ObjectID || current.ManualLocked {
			t.Fatalf("refresh did not replace unlocked %s", field)
		}
	}
	if other := metadataSelection(t, c, secondID); other.Revision != beforeSecond.Revision {
		t.Fatal("refresh changed another directory")
	}
	lyrics := refreshBinding(t, after, "lyrics")
	code, raw, _ := c.request(t, "PUT", "/api/v1/source-tracks/"+firstID+"/media-selection", map[string]any{"assetId": after.Target.AssetID, "assetVersion": after.Target.AssetVersion, "objectId": lyrics.ObjectID, "expectedRevision": after.Revision}, "refresh-manual-lyrics")
	if code != 200 {
		t.Fatalf("manual selection failed: %d %s", code, raw)
	}
	writeRefreshSidecars(t, first, "first", 3)
	locked := refresh("refresh-keeps-manual-lyrics")
	if current := refreshBinding(t, locked, "lyrics"); current.ObjectID != lyrics.ObjectID || !current.ManualLocked {
		t.Fatal("refresh replaced manually selected lyrics")
	}
	if refreshBinding(t, locked, "artwork").ObjectID == refreshBinding(t, after, "artwork").ObjectID {
		t.Fatal("refresh did not update unlocked artwork beside manual lyrics")
	}
	code, raw, _ = c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &providers) != nil {
		t.Fatalf("providers failed: %d %s", code, raw)
	}
	for _, provider := range providers.Items {
		if provider.ID == providerID && (provider.Configuration.ImportDefaults.ImportLyrics || provider.Configuration.ImportDefaults.ImportArtwork) {
			t.Fatal("refresh mutated saved import defaults")
		}
	}
	request := providerScanRequest(t, c, providerID)
	request["options"] = map[string]any{"refreshMetadata": true}
	if code, _, _ := c.request(t, "POST", "/api/v1/provider-instances/"+providerID+"/scan-jobs", request, "refresh-invalid-roots"); code != 400 {
		t.Fatal("multi-root refresh accepted", code)
	}
	request["rootIds"], request["mode"] = []string{"one"}, "incremental"
	if code, _, _ := c.request(t, "POST", "/api/v1/provider-instances/"+providerID+"/scan-jobs", request, "refresh-invalid-mode"); code != 400 {
		t.Fatal("incremental refresh accepted", code)
	}
}

func refreshBinding(t *testing.T, selection mediastore.Selection, field string) mediastore.Binding {
	t.Helper()
	for _, binding := range selection.Bindings {
		if binding.Field == field {
			return binding
		}
	}
	t.Fatalf("missing %s binding", field)
	return mediastore.Binding{}
}

func writeRefreshSidecars(t *testing.T, root, name string, version byte) {
	t.Helper()
	lyrics := append([]byte("[00:01.00]Synthetic lyrics revision "), '0'+version)
	picture := image.NewNRGBA(image.Rect(0, 0, int(version)+2, int(version)+2))
	picture.SetNRGBA(0, 0, color.NRGBA{R: version, A: 255})
	var cover bytes.Buffer
	if err := png.Encode(&cover, picture); err != nil {
		t.Fatal(err)
	}
	for extension, body := range map[string][]byte{".lrc": lyrics, ".png": cover.Bytes()} {
		if err := os.WriteFile(filepath.Join(root, name+extension), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

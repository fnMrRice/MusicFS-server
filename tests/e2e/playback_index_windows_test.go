//go:build windows

package e2e_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestWindowsPlaybackReadsCurrentFileUntilLibraryRefresh(t *testing.T) {
	const providerID = "b1548e96-3081-5c9c-8376-bf290e369d51"
	const name = "source.wav"
	root := t.TempDir()
	path := filepath.Join(root, name)
	original := fingerprintWave(1)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: build(t, "musicfs-plugin-local"), Enabled: true,
		Instances: []config.Instance{{InstanceID: providerID, Enabled: true, Config: config.InstanceConfig{
			MusicRoots: []config.MusicRoot{{ID: "root", Name: "Music", Path: root}},
		}}}}}
	disableLocalLyricsImport(&cfg)
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	first := scanProvider(t, c, providerID, "initial-windows-scan")[name]
	if first.ID == "" || first.AssetID == "" || first.Asset == nil || first.Asset.Version == "" {
		t.Fatalf("missing identity for the scanned track: %+v", first)
	}
	expectSameAsset := func(track library.Track) {
		t.Helper()
		if track.Availability != "available" || track.ID != first.ID || track.AssetID != first.AssetID || track.Asset == nil || track.Asset.Version != first.Asset.Version {
			t.Fatalf("expected the original indexed track and asset, got %+v", track)
		}
	}
	changedTime := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(path, changedTime, changedTime); err != nil {
		t.Fatal(err)
	}
	expectSameAsset(scanProvider(t, c, providerID, "windows-timestamp-refresh")[name])
	updated := fingerprintWave(2)
	if err := os.WriteFile(path, updated, 0600); err != nil {
		t.Fatal(err)
	}
	play := playback.CreateRequest{SourceTrackID: first.ID, Preferences: playback.Preferences{DSPExecution: "client"},
		Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
	session := albumRequest[playback.Description](t, c, "POST", "/playback/sessions", play, "windows-play-before-refresh", 201, "Playback")
	if session.PositionOriginMs != 0 || session.ResumeDecision != "continue" || session.SourceTrackID != first.ID {
		t.Fatalf("playback did not start from the beginning of the selected track: %+v", session)
	}
	verifyRanges(t, c, session, updated)
	expectSameAsset(albumRequest[library.Track](t, c, "GET", "/source-tracks/"+first.ID, nil, "", 200, "SourceTrack"))
	final := scanProvider(t, c, providerID, "windows-content-refresh")[name]
	if final.ID != first.ID || final.AssetID == "" || final.AssetID == first.AssetID || final.Asset == nil || final.Asset.Version == first.Asset.Version {
		t.Fatalf("refresh did not bind a new asset to the same track: %+v", final)
	}
	replacement := filepath.Join(root, "replacement.tmp")
	if err := os.WriteFile(replacement, updated, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	scanProvider(t, c, providerID, "windows-identical-refresh")
	page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
	if len(page.Items) != 1 || page.Items[0].ID != first.ID || page.Items[0].AssetID != final.AssetID || page.Items[0].Availability != "available" {
		t.Fatalf("same-content replacement changed the indexed source: %+v", page.Items)
	}
}

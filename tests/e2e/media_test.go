package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMediaActualServerPreviewSelectionRestartAndPlayback(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	source := filepath.Join(music, "media.wav")
	if err := os.WriteFile(source, wave, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Media fixture", Path: music}}}}}}}
	disableLocalLyricsImport(&cfg)
	var target mediastore.ContentTarget
	var lyrics, cover mediastore.ContentObject
	lyricBytes := []byte("[00:00.00]合成歌词\n[00:01.00]仅用于测试\n")
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	selectionPath := func() string { return "/api/v1/source-tracks/" + target.SourceTrackID + "/media-selection" }
	readSelection := func(t *testing.T, c *client) mediastore.Selection {
		t.Helper()
		code, raw, _ := c.request(t, "GET", selectionPath(), nil, "")
		var out mediastore.Selection
		if code != 200 || json.Unmarshal(raw, &out) != nil {
			t.Fatalf("selection: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "MediaSelection", raw)
		return out
	}
	selectObject := func(t *testing.T, c *client, object string, revision int64, key string) mediastore.Selection {
		t.Helper()
		input := map[string]any{"objectId": object, "assetId": target.AssetID, "assetVersion": target.AssetVersion, "expectedRevision": revision}
		code, raw, _ := c.request(t, "PUT", selectionPath(), input, key)
		var out mediastore.Selection
		if code != 200 || json.Unmarshal(raw, &out) != nil {
			t.Fatalf("select: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "MediaSelection", raw)
		return out
	}
	t.Run("upload-and-select", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		tracks := scanNCM(t, c)
		track := tracks["media.wav"]
		target = mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}
		if target.SourceTrackID == "" {
			t.Fatal("missing source")
		}
		if code, _, _ := c.request(t, "POST", "/api/v1/source-tracks/"+target.SourceTrackID+"/fingerprint-jobs", map[string]any{"startMs": 0, "durationMs": 10000}, "media-fp-disabled"); code != 415 {
			t.Fatal("fixture unexpectedly enabled fingerprinting")
		}
		lyrics = uploadActualMedia(t, c, target, "lyrics", "ZH-CN", "text/plain", lyricBytes, "media-lyrics-upload")
		cover = uploadActualMedia(t, c, target, "artwork", "", "image/png", pngBytes.Bytes(), "media-cover-upload")
		if lyrics.Metadata.Format != "lrc" || lyrics.Metadata.Language != "zh-cn" || cover.Metadata.Format != "png" {
			t.Fatal("actual formats were not preserved")
		}
		if selection := readSelection(t, c); selection.Revision != 0 || len(selection.Bindings) != 0 {
			t.Fatal("preview was automatically selected")
		}
		for id, body := range map[string][]byte{lyrics.ID: lyricBytes, cover.ID: pngBytes.Bytes()} {
			code, raw, headers := c.request(t, "GET", "/api/v1/media/"+id, nil, "")
			if code != 200 || !bytes.Equal(raw, body) || headers.Get("Cache-Control") != "no-store" || headers.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("media delivery mismatch")
			}
		}
		selectObject(t, c, lyrics.ID, 0, "media-lyrics-select")
		selected := selectObject(t, c, cover.ID, 1, "media-cover-select")
		if selected.Revision != 2 || len(selected.Bindings) != 2 {
			t.Fatal("selection revision or independent fields missing")
		}
		for _, binding := range selected.Bindings {
			if !binding.ManualLocked || !binding.Current || binding.State != "available" {
				t.Fatal("selection is not locked and current")
			}
		}
		input := map[string]any{"field": "lyrics", "language": "zh-CN", "expectedRevision": 2}
		code, raw, _ := c.request(t, "POST", selectionPath()+"/unlock", input, "media-lyrics-unlock")
		if code != 200 {
			t.Fatalf("unlock: %d %s", code, raw)
		}
		selected = readSelection(t, c)
		if selected.Revision != 3 {
			t.Fatal("unlock revision missing")
		}
		for _, binding := range selected.Bindings {
			if binding.Field == "lyrics" && (binding.ManualLocked || binding.ObjectID != lyrics.ID) {
				t.Fatal("unlock changed existing selection")
			}
		}
		play := playback.CreateRequest{SourceTrackID: target.SourceTrackID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
		code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", play, "media-playback-key")
		var description playback.Description
		if code != 201 || json.Unmarshal(raw, &description) != nil {
			t.Fatalf("playback after media upload: %d %s", code, raw)
		}
		verifyRanges(t, c, description, wave)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
			t.Fatal("playback stop failed")
		}
		actual, err := os.ReadFile(source)
		if err != nil || sha256.Sum256(actual) != sha256.Sum256(wave) {
			t.Fatal("media upload modified original audio")
		}
	})
	t.Run("restart-and-asset-change", func(t *testing.T) {
		if target.SourceTrackID == "" || lyrics.ID == "" {
			t.Fatal("initial phase failed")
		}
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		selection := readSelection(t, c)
		if selection.Revision != 3 || len(selection.Bindings) != 2 {
			t.Fatal("media selection did not survive restart")
		}
		for _, binding := range selection.Bindings {
			if !binding.Current || binding.ManualLocked != (binding.Field == "artwork") {
				t.Fatal("current state or manual locks did not survive restart")
			}
		}
		for _, object := range []mediastore.ContentObject{lyrics, cover} {
			code, raw, _ := c.request(t, "GET", "/api/v1/media/"+object.ID+"/info", nil, "")
			if code != 200 {
				t.Fatalf("media info after restart: %d %s", code, raw)
			}
			testkit.ValidateHTTP(t, "ContentObject", raw)
		}
		replay := uploadActualMedia(t, c, target, "lyrics", "ZH-CN", "text/plain", lyricBytes, "media-lyrics-upload")
		if replay.ID != lyrics.ID {
			t.Fatal("upload idempotency did not survive restart")
		}
		changed := append(append([]byte(nil), wave...), 0)
		if err := os.WriteFile(source, changed, 0600); err != nil {
			t.Fatal(err)
		}
		tracks := scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "media-source-rescan")
		if tracks["media.wav"].ID != target.SourceTrackID || tracks["media.wav"].AssetID == target.AssetID {
			t.Fatal("source identity or asset generation incorrect")
		}
		selection = readSelection(t, c)
		for _, binding := range selection.Bindings {
			if binding.Current || binding.State != "stale" || binding.ManualLocked != (binding.Field == "artwork") {
				t.Fatal("old asset binding became current or lost its lock")
			}
		}
		code, raw, _ := c.request(t, "GET", "/api/v1/media/"+lyrics.ID, nil, "")
		if code != 200 || !bytes.Equal(raw, lyricBytes) {
			t.Fatal("older authorized media became unreadable")
		}
		input := map[string]any{"objectId": lyrics.ID, "assetId": target.AssetID, "assetVersion": target.AssetVersion, "expectedRevision": selection.Revision}
		if code, _, _ := c.request(t, "PUT", selectionPath(), input, "media-stale-select"); code != 409 {
			t.Fatal("stale asset selected after rescan")
		}
	})
}

func uploadActualMedia(t *testing.T, c *client, target mediastore.ContentTarget, field, language, mime string, body []byte, key string) mediastore.ContentObject {
	t.Helper()
	query := url.Values{"sourceTrackId": {target.SourceTrackID}, "assetId": {target.AssetID}, "assetVersion": {target.AssetVersion}, "field": {field}, "language": {language}}
	r, err := http.NewRequest("POST", c.base+"/api/v1/media?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", c.base)
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("Content-Type", mime)
	response, err := c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var object mediastore.ContentObject
	if response.StatusCode != 201 || json.Unmarshal(raw, &object) != nil {
		t.Fatalf("media upload: %d %s", response.StatusCode, raw)
	}
	testkit.ValidateHTTP(t, "ContentObject", raw)
	return object
}

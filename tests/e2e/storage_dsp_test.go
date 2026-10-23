package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
)

func TestExternalStorageAndDSPServerRestartAndPortableRestore(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for external storage and DSP validation")
	}
	ctx := context.Background()
	provider, decoder := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg")
	storageWorker, gainWorker := build(t, "musicfs-plugin-storage-sqlite"), build(t, "musicfs-plugin-dsp-gain")
	hashFile := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	music := t.TempDir()
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(music, "external.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	const instance = "b1548e96-3081-5c9c-8376-bf290e369d51"
	cfg.Storage = config.StorageConfig{Execution: "process", Backend: "plugin", Executable: storageWorker, SHA256: hashFile(storageWorker)}
	cfg.AudioDSP = config.AudioDSPConfig{
		Enabled: true,
		Chain:   []audio.NodeSpec{{Name: "musicfs.dsp.gain", Version: 1, Params: map[string]float64{"gainDB": -6}}},
		Plugins: []dspworker.Config{{Name: "musicfs.dsp.gain", Executable: gainWorker, SHA256: hashFile(gainWorker), TimeoutMs: 5000}},
	}
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: instance, Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: decoder, Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	disableLocalLyricsImport(&cfg)
	path := writeYAMLConfiguration(t, cfg)
	if _, err = config.Load(path, map[string]string{"ADMIN_TOKEN": cfg.AdminToken}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(cfg.DataRoot); !os.IsNotExist(err) {
		t.Fatalf("configuration loading created storage before the data lock: %v", err)
	}
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	track := scanNCM(t, c)["external.wav"]
	if track.ID == "" {
		t.Fatal("external storage did not publish the scanned source")
	}
	request := playback.CreateRequest{SourceTrackID: track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}, {MIME: "audio/mpeg", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "external-storage-dsp-create")
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil || description.DeliveryMode != "processed" || len(description.EffectiveProcessing) != 2 || description.EffectiveProcessing[0].Type != "gain" || description.EffectiveProcessing[1].Type != "musicfs.dsp.gain" {
		t.Fatalf("external server did not apply DSP: status=%d %s", code, raw)
	}
	// 未启用声道转换时，处理链只包含请求增益和显式配置的 DSP 节点。
	configuredGain := description.EffectiveProcessing[1]
	if configuredGain.GainDB != -6 || configuredGain.Parameters["gainDB"] != -6 || configuredGain.Execution != "server" || configuredGain.Version != "1" {
		t.Fatalf("external server lost the configured DSP parameters: %+v", configuredGain)
	}
	pcm := fetchYAMLAudioPCM(t, c, ffmpeg, description.StreamURL)
	assertYAMLAudioDuration(t, pcm, 6000)
	stopManagedAudioPlayback(t, c, description.SessionID)
	op := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/"+instance+"/configuration", map[string]any{"name": "Persisted external provider"})
	waitLibraryOperation(t, c, op.ID)
	stop()

	statePath := config.PluginDatabasePath(cfg)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	infoBefore, err := os.Stat(statePath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := config.ReadDocument(path, map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(statePath)
	infoAfter, statErr := os.Stat(statePath)
	if err != nil || statErr != nil || !bytes.Equal(before, after) || !infoBefore.ModTime().Equal(infoAfter.ModTime()) {
		t.Fatalf("read-only external configuration load modified private storage: %v %v", err, statErr)
	}
	if doc.Config.Plugins[0].Instances[0].Name != "Persisted external provider" {
		t.Fatal("read-only external state failed to load persisted configuration")
	}
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	code, raw, _ = c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page library.Page
	if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 1 || page.Items[0].ID != track.ID {
		t.Fatalf("external storage lost source identity after restart: %d %s", code, raw)
	}
	code, raw, _ = c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	var managed plugincontrol.Snapshot
	if code != 200 || json.Unmarshal(raw, &managed) != nil || strings.Contains(string(raw), filepath.Base(storageWorker)) || strings.Contains(string(raw), filepath.Base(gainWorker)) {
		t.Fatalf("management snapshot exposed executable settings: %d %s", code, raw)
	}
	assertStorageCannotBeDisabled(t, c, managed)
	stop()

	bundle, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 完成后的备份必须能在插件移除后独立恢复。
	if err = os.Rename(storageWorker, storageWorker+".disabled"); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(gainWorker, gainWorker+".disabled"); err != nil {
		t.Fatal(err)
	}
	restored, err := backup.Restore(ctx, bundle.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(restored.Configuration, map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Storage.Backend != "sqlite" || loaded.Storage.Executable != "" || len(loaded.AudioDSP.Chain) != 0 || len(loaded.AudioDSP.Plugins) != 0 {
		t.Fatal("restore retained authority to execute external storage or DSP")
	}
}

func assertStorageCannotBeDisabled(t *testing.T, c *client, managed plugincontrol.Snapshot) {
	t.Helper()
	storageID := ""
	dspFound := false
	for _, plugin := range managed.Items {
		if plugin.Storage != nil {
			storageID = plugin.PluginID
			if !plugin.Required || plugin.StateMutable || plugin.Storage.Mode != "process" || plugin.Storage.RuntimeState != "ready" || plugin.Version != "0.1.0" || len(plugin.Instances) != 0 {
				t.Fatalf("mandatory storage metadata is invalid: %+v", plugin)
			}
		}
		if plugin.PluginID == "musicfs.dsp.gain" && plugin.DSP != nil {
			dspFound = true
			if plugin.StateMutable || !plugin.DSP.Configured || plugin.DSP.ChainState != "in_chain" || len(plugin.DSP.Nodes) != 1 || plugin.DSP.Nodes[0].Position != 1 || plugin.DSP.Nodes[0].Params["gainDB"] != -6 || plugin.DSP.Nodes[0].Mode != "builtin" {
				t.Fatalf("DSP startup configuration metadata is invalid: %+v", plugin)
			}
		}
	}
	if storageID == "" || !dspFound {
		t.Fatal("infrastructure plugins are absent from the management list")
	}
	for _, id := range []string{storageID, "musicfs.plugin.storage.sqlite", "musicfs.dsp.gain", "storage", "sqlite"} {
		request, err := http.NewRequest("PUT", c.base+"/api/v1/admin/plugins/"+id+"/state", strings.NewReader(`{"enabled":false,"mode":"force"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", c.base)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", c.csrf)
		request.Header.Set("Idempotency-Key", "cannot-disable-"+id)
		request.Header.Set("If-Match", `"`+managed.Revision+`"`)
		response, err := c.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		wantStatus, wantCode := http.StatusUnsupportedMediaType, "UNSUPPORTED"
		if id == "storage" || id == "sqlite" {
			wantStatus, wantCode = http.StatusNotFound, "NOT_FOUND"
		}
		if readErr != nil || response.StatusCode != wantStatus || !bytes.Contains(body, []byte(`"`+wantCode+`"`)) {
			t.Fatalf("mandatory storage disable was not rejected: id=%s status=%d %s err=%v", id, response.StatusCode, body, readErr)
		}
	}
	if code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, ""); code != http.StatusOK {
		t.Fatalf("storage became unavailable after rejected disable: %d %s", code, raw)
	}
}

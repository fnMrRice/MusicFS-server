package bootstrap_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/google/uuid"
)

func TestBuiltinServerStartsSourcesWithoutAnyPluginExecutable(t *testing.T) {
	cfg := serverConfig(t)
	music := t.TempDir()
	for _, id := range []string{"musicfs.plugin.source.localfiles", "musicfs.plugin.decoder.ncm", "musicfs.scraper.local", "musicfs.plugin.source.netease", "musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic", "musicfs.plugin.source.bilibili"} {
		instance := config.Instance{InstanceID: uuid.NewString(), Enabled: true}
		if id == "musicfs.plugin.source.localfiles" {
			instance.Permissions = &config.Permissions{ReadDirectories: []string{music}}
			instance.Config.MusicRoots = []config.MusicRoot{{ID: "music", Name: "Music", Path: music}}
		}
		cfg.Plugins = append(cfg.Plugins, config.Plugin{PluginID: id, Enabled: true, Executable: filepath.Join(t.TempDir(), "must-not-exist.exe"), Instances: []config.Instance{instance}})
	}
	cookies := withSessions(t, &cfg)
	base, stop := serve(t, cfg, bootstrap.ServeOptions{})
	defer stop()
	var snapshot plugincontrol.Snapshot
	if status := readJSON(t, base, "/api/v1/admin/plugins", cookies["admin"], &snapshot); status != 200 {
		t.Fatal(status)
	}
	seen := map[string]bool{}
	for _, p := range snapshot.Items {
		seen[p.PluginID] = true
		if p.Storage != nil {
			if p.Storage.Mode != "builtin" || p.Storage.RuntimeState != "ready" {
				t.Fatal("storage left the main process", p.Storage)
			}
			continue
		}
		if p.DSP != nil {
			continue
		}
		if p.Execution != "builtin" {
			t.Fatal("plugin left the main process", p.PluginID)
		}
		for _, i := range p.Instances {
			if i.RuntimeState != "ready" {
				t.Fatal("builtin could not start without a worker executable", p.PluginID, i.RuntimeState, i.Reason)
			}
		}
	}
	if seen["musicfs.plugin.content.music"] {
		t.Fatal("plugin management listed the retired music content type")
	}
	for _, id := range []string{
		"musicfs.plugin.analysis.chromaprint", "musicfs.plugin.tags.flac", "musicfs.plugin.matching.standard", "musicfs.plugin.transform.thumbnail",
		"musicfs.plugin.content.cd_album", "musicfs.plugin.content.asmr", "musicfs.plugin.content.radio",
	} {
		if !seen[id] {
			t.Fatal("missing builtin plugin", id)
		}
	}
}

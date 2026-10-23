package bootstrap_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/google/uuid"
)

type registeredProcessor struct {
	audioruntime.Unavailable
	gate *plugincontrol.Gate
}

func (*registeredProcessor) Available() bool       { return true }
func (*registeredProcessor) PluginVersion() string { return "3.2.1" }
func (*registeredProcessor) AudioDecoder() audioformat.Decoder {
	return audioformat.Decoder{Detector: audioformat.Detector{PluginID: "test.bootstrap.audio"}}
}
func (p *registeredProcessor) SetGate(g *plugincontrol.Gate) { p.gate = g }

func TestRegisteredAudioFactoryOwnsRuntimeAndGate(t *testing.T) {
	const id = "test.bootstrap.audio"
	identity := &pluginproto.PluginIdentity{ID: id, Name: "Independent audio", LocalizedNames: map[string]string{}, Features: []string{"audio-decode"}}
	if err := pluginconfig.Register(pluginconfig.Declaration{ID: id, Role: "audio", Version: "3.2.1", Identity: identity, AudioProcessing: true}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	processor := &registeredProcessor{}
	if err := bootstrap.RegisterAudioFactory(id, bootstrap.AudioFactory{Processing: true, Create: func(context.Context, config.Config, config.Plugin, config.Instance) (bootstrap.AudioInstance, error) {
		calls.Add(1)
		return bootstrap.AudioInstance{Processor: processor}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	cfg := serverConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: id, Enabled: true, Instances: []config.Instance{{InstanceID: uuid.NewString(), Enabled: true}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Enabled: false, Instances: []config.Instance{{InstanceID: uuid.NewString(), Enabled: true}}},
	}
	cookies := withSessions(t, &cfg)
	base, stop := serve(t, cfg, bootstrap.ServeOptions{})
	defer stop()
	var snapshot plugincontrol.Snapshot
	if readJSON(t, base, "/api/v1/admin/plugins", cookies["admin"], &snapshot) != 200 {
		t.Fatal("management snapshot unavailable")
	}
	if calls.Load() != 1 || processor.gate == nil || !processor.gate.Accepting() {
		t.Fatal("factory runtime was not assigned its active gate")
	}
	for _, plugin := range snapshot.Items {
		if plugin.PluginID == id {
			if plugin.Version != "3.2.1" || len(plugin.Instances) != 1 || plugin.Instances[0].RuntimeState != "ready" {
				t.Fatalf("registered processor is not ready: %+v", plugin)
			}
			return
		}
	}
	t.Fatal("registered processor is missing from management")
}

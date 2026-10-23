package onlinehost_test

import (
	"context"
	"io"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type disabledAudioProvider struct {
	provider.UnimplementedClient
	calls int
}

func (p *disabledAudioProvider) Descriptor() pluginproto.OnlineDescriptor {
	d := pluginproto.OnlinePolicy(online.Bilibili)
	d.Plugin = &pluginproto.PluginIdentity{ID: pluginproto.OnlinePluginID(online.Bilibili), Name: "Metadata fixture", LocalizedNames: map[string]string{}, Features: []string{"music-source"}}
	for i := range d.Capabilities {
		if d.Capabilities[i].Name == "stream.resolve" {
			d.Capabilities[i].Supported = false
			d.Capabilities[i].Reason = "Disabled by fixture"
		}
	}
	return d
}
func (p *disabledAudioProvider) Metadata(context.Context, string) (online.Track, error) {
	p.calls++
	return online.Track{}, nil
}
func (p *disabledAudioProvider) DescribeAudio(context.Context, string) (pluginproto.OnlineAudio, error) {
	p.calls++
	return pluginproto.OnlineAudio{}, nil
}
func (p *disabledAudioProvider) OpenAudio(context.Context, string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	p.calls++
	return pluginproto.OnlineAudio{}, nil, nil
}

func TestAudioInterfaceDoesNotOverrideDisabledCapability(t *testing.T) {
	p := &disabledAudioProvider{}
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "network", Platform: online.Bilibili, ConfigRevision: "fixture", Enabled: true, Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.New(nil)
	defer queue.Close()
	lib := library.New(nil, queue, nil, nil)
	if err = host.ConfigureAudio(lib); err != nil {
		t.Fatal(err)
	}
	_, err = host.Resolve(context.Background(), "owner", "network", "audio:1")
	requireBilibiliCode(t, err, "UNSUPPORTED")
	if p.calls != 0 {
		t.Fatal("disabled stream capability reached a provider implementation")
	}
}

package provideronline_test

import (
	"context"
	"strings"
	"testing"

	provideronline "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestPluginOwnsDisplayIdentityAndAcceptsCustomTranslations(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		d := provideronline.Describe(platform)
		if err := d.Validate(platform); err != nil {
			t.Fatal(err)
		}
		d.Plugin.Name = "Custom Music"
		d.Plugin.LocalizedNames = map[string]string{"zh-Hans": "定制音乐", "fr-CA": "Musique personnalisée"}
		if err := d.Validate(platform); err != nil {
			t.Fatal("host rejected plugin-owned display names", err)
		}
	}
}

func TestOnlineDescriptorRejectsInvalidIdentityAndPreservesCapabilityPolicy(t *testing.T) {
	for name, mutate := range map[string]func(*pluginproto.OnlineDescriptor){
		"missing":                func(d *pluginproto.OnlineDescriptor) { d.Plugin = nil },
		"identity":               func(d *pluginproto.OnlineDescriptor) { d.Plugin.ID = "musicfs.plugin.source.qqmusic" },
		"empty":                  func(d *pluginproto.OnlineDescriptor) { d.Plugin.Name = "" },
		"whitespace":             func(d *pluginproto.OnlineDescriptor) { d.Plugin.Name = " Name" },
		"control":                func(d *pluginproto.OnlineDescriptor) { d.Plugin.Name = "Bad\nName" },
		"long":                   func(d *pluginproto.OnlineDescriptor) { d.Plugin.Name = strings.Repeat("音", 129) },
		"missing translations":   func(d *pluginproto.OnlineDescriptor) { d.Plugin.LocalizedNames = nil },
		"missing features":       func(d *pluginproto.OnlineDescriptor) { d.Plugin.Features = nil },
		"missing source feature": func(d *pluginproto.OnlineDescriptor) { d.Plugin.Features = []string{"another-feature"} },
		"duplicate feature":      func(d *pluginproto.OnlineDescriptor) { d.Plugin.Features = []string{"music-source", "music-source"} },
		"invalid feature":        func(d *pluginproto.OnlineDescriptor) { d.Plugin.Features = []string{"music-source", "bad_feature"} },
		"long feature": func(d *pluginproto.OnlineDescriptor) {
			d.Plugin.Features = []string{"music-source", strings.Repeat("a", 65)}
		},
		"invalid locale":    func(d *pluginproto.OnlineDescriptor) { d.Plugin.LocalizedNames["zh_CN"] = "名称" },
		"invalid language":  func(d *pluginproto.OnlineDescriptor) { d.Plugin.LocalizedNames["invalidlanguage"] = "Name" },
		"empty translation": func(d *pluginproto.OnlineDescriptor) { d.Plugin.LocalizedNames["fr"] = "" },
		"duplicate locale":  func(d *pluginproto.OnlineDescriptor) { d.Plugin.LocalizedNames["ZH"] = "名称" },
		"stale revision":    func(d *pluginproto.OnlineDescriptor) { d.SchemaRevision = "online-authenticated-v2" },
		"capability":        func(d *pluginproto.OnlineDescriptor) { d.Capabilities[len(d.Capabilities)-1].Supported = true },
		"config":            func(d *pluginproto.OnlineDescriptor) { d.ConfigDefaults["unverified"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			d := provideronline.Describe(online.Netease)
			mutate(&d)
			if d.Validate(online.Netease) == nil {
				t.Fatal("invalid descriptor accepted")
			}
		})
	}
}

func TestOnlineWorkerRejectsPreviousHandshakeRevision(t *testing.T) {
	peer := worker(t, nil)
	request := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: "1.0", InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", Nonce: strings.Repeat("a", 48), ConfigRevision: "online-authenticated-v2"}, UserID: "owner", TimeoutMs: 3000}
	assertCode(t, peer.Call(context.Background(), "host.hello", request, nil), "PROTOCOL_MISMATCH")
	hello(t, peer)
}

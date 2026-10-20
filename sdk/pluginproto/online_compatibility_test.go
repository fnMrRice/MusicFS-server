package pluginproto_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func compatibleOnlinePlugin() (pluginproto.Manifest, pluginproto.OnlineDescriptor) {
	descriptor := pluginproto.OnlinePolicy(online.Netease)
	descriptor.Plugin = &pluginproto.PluginIdentity{
		ID: pluginproto.OnlinePluginID(online.Netease), Name: "Metadata provider",
		LocalizedNames: map[string]string{}, Features: []string{"music-source"},
	}
	descriptor.Capabilities = []online.Capability{{Name: "metadata.track", Supported: true, Reason: "Metadata only"}}
	manifest := pluginproto.Manifest{
		PluginID: descriptor.Plugin.ID, Plugin: descriptor.Plugin.Clone(),
		SupportsMultipleInstances: true, PluginVersion: "2.3.4-beta.2+build.19",
		ProtocolVersion: pluginproto.ProtocolVersion, Roles: []string{"provider"},
		Capabilities: []string{"metadata.track"}, MaxPageItems: online.MaxPageItems,
		MaxMessageBytes: pluginproto.MaxMessageBytes,
	}
	return manifest, descriptor
}

func TestOnlineIndependentReleaseAndCapabilitySubset(t *testing.T) {
	manifest, descriptor := compatibleOnlinePlugin()
	if err := pluginproto.ValidateOnlineDescriptor(manifest, descriptor, online.Netease); err != nil {
		t.Fatalf("compatible independent release rejected: %v", err)
	}
	descriptor.Capabilities = append(descriptor.Capabilities, online.Capability{
		Name: "stream.resolve", Supported: false, Reason: "Unavailable in this release",
	})
	if err := pluginproto.ValidateOnlineDescriptor(manifest, descriptor, online.Netease); err != nil {
		t.Fatalf("explicit unavailable capability rejected: %v", err)
	}
	manifest.Capabilities = []string{}
	descriptor.Capabilities = []online.Capability{}
	if err := pluginproto.ValidateOnlineDescriptor(manifest, descriptor, online.Netease); err != nil {
		t.Fatalf("empty safe capability subset rejected: %v", err)
	}
}

func TestOnlineIncompatibleDeclarationsAreRejected(t *testing.T) {
	cases := map[string]func(*pluginproto.Manifest, *pluginproto.OnlineDescriptor){
		"wire version":    func(m *pluginproto.Manifest, _ *pluginproto.OnlineDescriptor) { m.ProtocolVersion = "2.0" },
		"schema revision": func(_ *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) { d.SchemaRevision = "online-v999" },
		"authentication":  func(_ *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) { d.Authentication = "anonymous" },
		"unknown capability": func(m *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) {
			m.Capabilities = []string{"filesystem.write"}
			d.Capabilities = []online.Capability{{Name: "filesystem.write", Supported: true}}
		},
		"unverified capability": func(m *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) {
			m.Capabilities = []string{"stream.resolve"}
			d.Capabilities = []online.Capability{{Name: "stream.resolve", Supported: true}}
		},
		"duplicate manifest capability": func(m *pluginproto.Manifest, _ *pluginproto.OnlineDescriptor) {
			m.Capabilities = append(m.Capabilities, m.Capabilities[0])
		},
		"duplicate descriptor capability": func(_ *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) {
			d.Capabilities = append(d.Capabilities, d.Capabilities[0])
		},
		"manifest descriptor disagreement": func(_ *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) {
			d.Capabilities[0].Name = "search.tracks"
		},
		"display identity disagreement": func(_ *pluginproto.Manifest, d *pluginproto.OnlineDescriptor) {
			d.Plugin.Name = "Different plugin"
		},
		"expanded resource limit": func(m *pluginproto.Manifest, _ *pluginproto.OnlineDescriptor) {
			m.MaxMessageBytes++
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest, descriptor := compatibleOnlinePlugin()
			mutate(&manifest, &descriptor)
			if err := pluginproto.ValidateOnlineDescriptor(manifest, descriptor, online.Netease); err == nil {
				t.Fatal("incompatible declaration accepted")
			}
		})
	}
}

func TestPluginVersions(t *testing.T) {
	for _, version := range []string{"0.1.0", "1.0.0", "12.34.56-rc.1+build.003", "1.2.3-0", "1.2.3-alpha-1"} {
		if !pluginproto.ValidPluginVersion(version) {
			t.Errorf("valid independent release rejected: %q", version)
		}
	}
	for _, version := range []string{"", "dev", "1.0", "v1.0.0", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-", "1.2.3+", "1.2.3 \n"} {
		t.Run(version, func(t *testing.T) {
			manifest, descriptor := compatibleOnlinePlugin()
			manifest.PluginVersion = version
			if pluginproto.ValidPluginVersion(version) || pluginproto.ValidateOnlineDescriptor(manifest, descriptor, online.Netease) == nil {
				t.Errorf("invalid plugin release accepted: %q", version)
			}
		})
	}
}

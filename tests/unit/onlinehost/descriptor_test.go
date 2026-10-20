package onlinehost_test

import (
	"context"
	"testing"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
)

type describedProvider struct {
	*blocked
	descriptor pluginproto.OnlineDescriptor
}

func (p *describedProvider) Descriptor() pluginproto.OnlineDescriptor { return p.descriptor }

func TestOnlineIdentitySurvivesRuntimeChangesWithDetachedCopies(t *testing.T) {
	p := &describedProvider{blocked: &blocked{}, descriptor: onlineplugins.Describe(online.Netease)}
	p.descriptor.Plugin.Name = "Custom provider"
	p.descriptor.Plugin.LocalizedNames["fr"] = "Nom personnalisé"
	r := onlinehost.Runtime{Owner: "owner", InstanceID: "instance", Name: "Instance label", Platform: online.Netease, ConfigRevision: "r1", Enabled: true, Provider: p}
	s, err := onlinehost.New([]onlinehost.Runtime{r})
	if err != nil {
		t.Fatal(err)
	}
	read := func(want string) []onlinehost.Instance {
		t.Helper()
		items, err := s.Instances(context.Background(), "owner")
		if err != nil || len(items) != 1 || items[0].Plugin == nil || items[0].Plugin.Name != want || items[0].Plugin.LocalizedNames["fr"] != "Nom personnalisé" || items[0].Name != want {
			t.Fatalf("identity changed: %+v %v", items, err)
		}
		return items
	}
	p.descriptor.Plugin.Name = "Changed outside host"
	items := read("Custom provider")
	items[0].Plugin.Name = "Changed API copy"
	items[0].Plugin.LocalizedNames["fr"] = "Changed translation"
	items[0].ConfigSchema["properties"].(map[string]any)["injected"] = true
	items = read("Custom provider")
	if len(items[0].ConfigSchema["properties"].(map[string]any)) != 0 {
		t.Fatal("configuration map aliases the host cache")
	}
	r.Enabled, r.Provider, r.ConfigRevision = false, nil, "r2"
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	if read("Custom provider")[0].Availability != "offline" {
		t.Fatal("disabled instance remained available")
	}
	r.Enabled, r.ConfigRevision = true, "r3"
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	read("Custom provider")
	p.descriptor.Plugin.Name = "New version"
	r.Provider = p
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	read("New version")
	p.descriptor.Plugin.ID = "musicfs.plugin.source.qqmusic"
	if err := s.UpdateRuntime(r); err == nil {
		t.Fatal("runtime replacement accepted another plugin identity")
	}
	read("New version")
}

func TestNeverStartedOnlineInstanceHasNoInventedPluginName(t *testing.T) {
	s, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "offline", Platform: online.Netease, ConfigRevision: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.Instances(context.Background(), "owner")
	if err != nil || len(items) != 1 || items[0].Plugin != nil {
		t.Fatal("never-started instance invented plugin metadata", items, err)
	}
}

func TestOnlineInstancePublishesDeclaredCapabilitySubset(t *testing.T) {
	p := &describedProvider{blocked: &blocked{}, descriptor: onlineplugins.Describe(online.Netease)}
	p.descriptor.Capabilities = []online.Capability{{Name: "metadata.track", Supported: true, Reason: "Metadata-only release"}}
	s, err := onlinehost.New([]onlinehost.Runtime{{
		Owner: "owner", InstanceID: "subset", Platform: online.Netease,
		ConfigRevision: "r1", Enabled: true, Provider: p,
	}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.Instances(context.Background(), "owner")
	if err != nil || len(items) != 1 || len(items[0].Capabilities) != 1 || items[0].Capabilities[0].Name != "metadata.track" {
		t.Fatalf("host substituted platform defaults for plugin capabilities: %+v %v", items, err)
	}
}

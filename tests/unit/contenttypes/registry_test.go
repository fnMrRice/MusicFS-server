package contenttypes_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/cdalbum"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
)

func TestRegistryListsBuiltinDeclarationsInRegistrationOrder(t *testing.T) {
	registry, err := contenttypes.New(music.Descriptor(), cdalbum.Descriptor(), asmr.Descriptor(), radio.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	listed := registry.List()
	if len(listed) != 4 || listed[0].ID != "music" || listed[0].Library != nil {
		t.Fatalf("unexpected declaration order: %+v", listed)
	}
	libraries := []struct {
		id, name, icon string
		order          int
		optional       bool
	}{{"cd_album", "CD专辑", "disc", 10, true}, {"asmr", "ASMR", "headphones", 20, false}, {"radio", "电台", "radio", 30, false}}
	for i, want := range libraries {
		item := listed[i+1]
		if item.ID != want.id || item.Name != want.name || item.Library == nil ||
			item.Library.Icon != want.icon || item.Library.Order != want.order || item.Library.TrackListOptional != want.optional {
			t.Fatalf("%s has unexpected library declaration: %+v", want.id, item)
		}
	}
	wantRoles := []string{"circle", "voice_actor", "scenario", "illustrator"}
	gotRoles := []string{}
	for _, role := range listed[2].CreditRoles {
		gotRoles = append(gotRoles, role.ID)
	}
	if !reflect.DeepEqual(gotRoles, wantRoles) || listed[2].ExternalIDNamespaces[0].ID != "dlsite" {
		t.Fatalf("unexpected ASMR metadata: %+v", listed[2])
	}
	for _, descriptor := range listed {
		if descriptor.PluginVersion != "0.1.0" || descriptor.PluginID != "musicfs.plugin.content."+descriptor.ID {
			t.Fatalf("unexpected built-in plugin identity: %+v", descriptor)
		}
		found, ok := registry.Get(descriptor.ID)
		if !ok || !reflect.DeepEqual(found, descriptor) {
			t.Fatalf("lookup disagrees with listing: %+v", found)
		}
	}
	if _, ok := registry.Get("unknown"); ok {
		t.Fatal("unknown content type was found")
	}
}

func TestRegistryRejectsInvalidOrDuplicateDeclarations(t *testing.T) {
	invalid := music.Descriptor()
	invalid.Views = []string{"custom-script"}
	sameID := asmr.Descriptor()
	sameID.ID = music.TypeID
	samePlugin := asmr.Descriptor()
	samePlugin.PluginID = music.PluginID
	tests := []struct {
		name        string
		descriptors []contenttype.Descriptor
		message     string
	}{
		{"invalid", []contenttype.Descriptor{invalid}, "unsupported content type view"},
		{"duplicate type", []contenttype.Descriptor{music.Descriptor(), sameID}, "duplicate content type id"},
		{"duplicate plugin", []contenttype.Descriptor{music.Descriptor(), samePlugin}, "duplicate content type plugin id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := contenttypes.New(test.descriptors...)
			if err == nil || !strings.Contains(err.Error(), test.message) || registry != nil {
				t.Fatalf("expected rejected registry, got %v, %v", registry, err)
			}
		})
	}
}

func TestRegistryProtectsStoredDeclarationsFromCallerMutation(t *testing.T) {
	input := music.Descriptor()
	registry, err := contenttypes.New(input)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&input)
	listed := registry.List()
	mutate(&listed[0])
	lookedUp, _ := registry.Get(music.TypeID)
	mutate(&lookedUp)
	stored, ok := registry.Get(music.TypeID)
	if !ok || !reflect.DeepEqual(stored, music.Descriptor()) {
		t.Fatalf("caller mutation changed stored declaration: %+v", stored)
	}
}

func TestRegistryAcceptsInjectedTypesAndEmptyRegistry(t *testing.T) {
	custom := contenttype.Descriptor{
		ID: "audiobook", PluginID: "example.content.audiobook", PluginVersion: "2.0.0",
		Name: "有声书", Views: []string{contenttype.ViewWorkDetail},
	}
	registry, err := contenttypes.New(custom)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := registry.Get("audiobook"); !ok || got.Name != "有声书" {
		t.Fatalf("injected type was not found: %+v", got)
	}
	empty, err := contenttypes.New()
	if err != nil || empty.List() == nil || len(empty.List()) != 0 {
		t.Fatalf("unexpected empty registry: %v, %v", empty, err)
	}
	var missing *contenttypes.Registry
	if missing.List() == nil || len(missing.List()) != 0 {
		t.Fatal("nil registry did not return an empty list")
	}
	if _, ok := missing.Get("music"); ok {
		t.Fatal("nil registry returned a descriptor")
	}
}

func TestBuiltinFactoriesReturnIndependentDeclarations(t *testing.T) {
	for name, factory := range map[string]func() contenttype.Descriptor{
		"music": music.Descriptor,
		"asmr":  asmr.Descriptor,
	} {
		t.Run(name, func(t *testing.T) {
			before := factory()
			other := factory()
			mutate(&other)
			if !reflect.DeepEqual(before, factory()) {
				t.Fatal("factory shared mutable declaration state")
			}
		})
	}
}

func mutate(descriptor *contenttype.Descriptor) {
	descriptor.ID = "changed"
	descriptor.CreditRoles[0].ID = "changed"
	descriptor.ExternalIDNamespaces[0].Name = "修改后的命名空间"
	descriptor.Views[0] = "changed"
}

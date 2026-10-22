package contenttype_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
)

func validDescriptor() contenttype.Descriptor {
	return contenttype.Descriptor{
		ID: "spoken_audio", PluginID: "example.content.spoken-audio", PluginVersion: "1.2.3-rc.1+build.5",
		Name: "演播作品", Description: "演播作品的内容声明。\n包含章节资料。",
		CreditRoles:          []contenttype.NamedValue{{ID: "narrator", Name: "演播者"}},
		ExternalIDNamespaces: []contenttype.NamedValue{{ID: "catalog_number", Name: "目录编号"}},
		Views:                []string{contenttype.ViewWorkList, contenttype.ViewWorkDetail},
	}
}

func TestDescriptorAcceptsIndependentTypeDeclaration(t *testing.T) {
	descriptor := validDescriptor()
	before := descriptor.Clone()
	if err := descriptor.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, descriptor) {
		t.Fatal("validation changed the declaration")
	}
	descriptor.CreditRoles = nil
	descriptor.ExternalIDNamespaces = []contenttype.NamedValue{}
	descriptor.Description = ""
	descriptor.Views = []string{contenttype.ViewWorkDetail}
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("optional metadata should be allowed: %v", err)
	}
}

func TestDescriptorRejectsInvalidDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*contenttype.Descriptor)
	}{
		{"empty type", func(d *contenttype.Descriptor) { d.ID = "" }},
		{"uppercase type", func(d *contenttype.Descriptor) { d.ID = "Music" }},
		{"type whitespace", func(d *contenttype.Descriptor) { d.ID = " music" }},
		{"type separator", func(d *contenttype.Descriptor) { d.ID = "spoken/audio" }},
		{"long type", func(d *contenttype.Descriptor) { d.ID = strings.Repeat("a", 65) }},
		{"empty plugin", func(d *contenttype.Descriptor) { d.PluginID = "" }},
		{"unqualified plugin", func(d *contenttype.Descriptor) { d.PluginID = "plugin" }},
		{"invalid plugin segment", func(d *contenttype.Descriptor) { d.PluginID = "example..plugin" }},
		{"long plugin", func(d *contenttype.Descriptor) { d.PluginID = "example." + strings.Repeat("a", 121) }},
		{"invalid version", func(d *contenttype.Descriptor) { d.PluginVersion = "1.2" }},
		{"invalid prerelease", func(d *contenttype.Descriptor) { d.PluginVersion = "1.2.3-01" }},
		{"empty name", func(d *contenttype.Descriptor) { d.Name = "" }},
		{"name whitespace", func(d *contenttype.Descriptor) { d.Name = " 演播作品" }},
		{"name newline", func(d *contenttype.Descriptor) { d.Name = "演播\n作品" }},
		{"name length", func(d *contenttype.Descriptor) { d.Name = strings.Repeat("音", 129) }},
		{"invalid utf8", func(d *contenttype.Descriptor) { d.Name = string([]byte{0xff}) }},
		{"description length", func(d *contenttype.Descriptor) { d.Description = strings.Repeat("音", 2049) }},
		{"description control", func(d *contenttype.Descriptor) { d.Description = "作品\x00资料" }},
		{"invalid role", func(d *contenttype.Descriptor) { d.CreditRoles[0].ID = "Voice Actor" }},
		{"empty role name", func(d *contenttype.Descriptor) { d.CreditRoles[0].Name = "" }},
		{"duplicate role", func(d *contenttype.Descriptor) { d.CreditRoles = append(d.CreditRoles, d.CreditRoles[0]) }},
		{"role count", func(d *contenttype.Descriptor) { d.CreditRoles = make([]contenttype.NamedValue, 65) }},
		{"invalid namespace", func(d *contenttype.Descriptor) { d.ExternalIDNamespaces[0].ID = "External.ID" }},
		{"empty namespace name", func(d *contenttype.Descriptor) { d.ExternalIDNamespaces[0].Name = "" }},
		{"duplicate namespace", func(d *contenttype.Descriptor) {
			d.ExternalIDNamespaces = append(d.ExternalIDNamespaces, d.ExternalIDNamespaces[0])
		}},
		{"namespace count", func(d *contenttype.Descriptor) { d.ExternalIDNamespaces = make([]contenttype.NamedValue, 65) }},
		{"no views", func(d *contenttype.Descriptor) { d.Views = nil }},
		{"unknown view", func(d *contenttype.Descriptor) { d.Views[0] = "custom-script" }},
		{"duplicate view", func(d *contenttype.Descriptor) { d.Views[1] = d.Views[0] }},
		{"too many views", func(d *contenttype.Descriptor) { d.Views = append(d.Views, contenttype.ViewWorkList) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor := validDescriptor()
			test.mutate(&descriptor)
			if err := descriptor.Validate(); err == nil {
				t.Fatal("expected invalid declaration to be rejected")
			}
		})
	}
}

func TestCloneProtectsAllMutableCollections(t *testing.T) {
	original := validDescriptor()
	cloned := original.Clone()
	cloned.CreditRoles[0].Name = "修改后的角色"
	cloned.ExternalIDNamespaces[0].ID = "changed"
	cloned.Views[0] = "changed"
	if !reflect.DeepEqual(original, validDescriptor()) {
		t.Fatal("changing the clone changed the original declaration")
	}
}

func TestCloneNormalizesEmptyCollections(t *testing.T) {
	cloned := (contenttype.Descriptor{}).Clone()
	if cloned.CreditRoles == nil || cloned.ExternalIDNamespaces == nil || cloned.Views == nil {
		t.Fatal("empty declaration collections should be non-nil")
	}
}

func TestValidIDUsesSharedStableIdentifierRules(t *testing.T) {
	for _, value := range []string{"music", "voice_actor", "spoken-audio", "track2"} {
		if !contenttype.ValidID(value) {
			t.Fatalf("valid identifier rejected: %q", value)
		}
	}
	for _, value := range []string{"", "Music", "dl.site", "2track", "音声", " space", strings.Repeat("a", 65)} {
		if contenttype.ValidID(value) {
			t.Fatalf("invalid identifier accepted: %q", value)
		}
	}
}

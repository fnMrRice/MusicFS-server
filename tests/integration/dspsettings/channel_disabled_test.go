package dspsettings_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestDisabledChannelPluginRejectsEditingAndIgnoresInheritedHistory(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := settingsConfig()
	cfg.DisableChannelMix = true
	cfg.OutputLayouts = []string{"stereo"}
	service := dspsettings.New(store, func() dspsettings.Config { return cfg })
	for _, alias := range []string{"channelmix", "musicfs.dsp.channelmix", "musicfs.plugin.decoder.channelmix"} {
		options := cfg.Defaults
		options.Chain = []audio.NodeSpec{{Name: alias, Version: 1}}
		if _, err := service.Validate(t.Context(), options); err == nil {
			t.Fatal("disabled channel alias passed validation", alias)
		}
	}
	legacy := cfg.Defaults
	legacy.OutputLayout = "7.1"
	legacy.Chain = []audio.NodeSpec{{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}}}
	if _, err := store.PutPluginSettings(t.Context(), dspsettings.GlobalOwner, dspsettings.GlobalStorageID, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": legacy}); err != nil {
		t.Fatal(err)
	}
	revision := int64(0)
	page, err := service.Update(t.Context(), "listener", dspsettings.Request{ExpectedRevision: &revision, SchemaRevision: dspsettings.SchemaRevision, InheritGlobal: true}, "inherit-disabled-channels")
	if err != nil || !page.UsesGlobal {
		t.Fatal("disabled channel history blocked inheritance", err)
	}
	for _, node := range page.Catalog {
		if node.Name == "channelmix" {
			t.Fatal("disabled channel plugin advertised as editable")
		}
	}
	if _, err := service.Validate(t.Context(), cfg.Defaults); err != nil {
		t.Fatal("disabled channel plugin blocked ordinary effects settings", err)
	}
}

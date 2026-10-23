package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func TestImportArtworkDefaultsOverridesAndMissingScraper(t *testing.T) {
	declaration := pluginconfig.LocalFiles()
	if !declaration.ImportDefaults.ImportArtwork || declaration.SchemaRevision != "local-import-v7" {
		t.Fatal("artwork import must default to enabled")
	}
	if err := declaration.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		patch := pluginproto.ImportOverrides{ImportArtwork: &enabled}
		cloned := patch.Clone()
		*cloned.ImportArtwork = !enabled
		if *patch.ImportArtwork != enabled {
			t.Fatal("cloned artwork override shares mutable storage")
		}
		merged, err := pluginproto.MergeImportOptions(declaration.ImportDefaults, patch)
		if err != nil || merged.ImportArtwork != enabled || declaration.ValidateOptions(merged) != nil {
			t.Fatal(merged, err)
		}
		value := "false"
		if enabled {
			value = "true"
		}
		raw := strings.Replace(minimalYAML+pluginYAML, "importDefaults: {importLyrics: false}", "importDefaults: {importLyrics: false, importArtwork: "+value+"}", 1)
		doc, err := parse(t, raw)
		if err != nil {
			t.Fatalf("artwork import rejected configuration without scraper: %v", err)
		}
		instance := doc.Config.Plugins[0].Instances[0]
		options, err := config.EffectiveImportDefaults("musicfs.plugin.source.localfiles", instance)
		if err != nil || options.ImportArtwork != enabled {
			t.Fatal(options, err)
		}
		root := instance.Config.MusicRoots[0]
		reverse := !enabled
		root.ImportDefaults = &pluginproto.ImportOverrides{ImportArtwork: &reverse}
		options, err = config.EffectiveRootImportDefaults("musicfs.plugin.source.localfiles", instance, root)
		if err != nil || options.ImportArtwork == enabled {
			t.Fatal("root artwork override was ignored", options, err)
		}
	}
	for _, raw := range []string{`{"importArtwork":null}`, `{"importArtwork":"true"}`, `{"importArtwork":1}`} {
		var patch pluginproto.ImportOverrides
		if json.Unmarshal([]byte(raw), &patch) == nil {
			t.Fatal("invalid artwork override accepted", raw)
		}
	}
}

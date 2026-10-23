package config_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func TestImportLyricsDefaultsPreserveExplicitOptOutAndRequireScraper(t *testing.T) {
	declaration := pluginconfig.LocalFiles()
	if !declaration.ImportDefaults.ImportLyrics || declaration.SchemaRevision != "local-import-v7" {
		t.Fatal("lyrics import must default to enabled")
	}
	if err := declaration.Validate(); err != nil {
		t.Fatal(err)
	}
	properties := declaration.ImportOptionsSchema["properties"].(map[string]any)
	lyrics := properties["importLyrics"].(map[string]any)
	if lyrics["default"] != true || lyrics["title"] != "导入歌词" {
		t.Fatal("lyrics schema differs from the effective default")
	}

	withoutOverride := strings.Replace(minimalYAML+pluginYAML, "        importDefaults: {importLyrics: false}\n", "", 1)
	if _, err := parse(t, withoutOverride); err == nil || !strings.Contains(err.Error(), "requires a configured scraper") {
		t.Fatalf("default lyrics import accepted missing scraper: %v", err)
	}
	withScraper := withoutOverride + strings.TrimPrefix(scraperYAML, "plugins:\n")
	for name, tc := range map[string]struct {
		raw     string
		enabled bool
	}{
		"new-directory-default":     {withScraper, true},
		"existing-instance-opt-out": {minimalYAML + pluginYAML, false},
		"existing-root-opt-out":     {strings.Replace(withScraper, "              name: 中文音乐", "              name: 中文音乐\n              importDefaults: {importLyrics: false}", 1), false},
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := parse(t, tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			instance := doc.Config.Plugins[0].Instances[0]
			options, err := config.EffectiveRootImportDefaults("musicfs.plugin.source.localfiles", instance, instance.Config.MusicRoots[0])
			if err != nil || options.ImportLyrics != tc.enabled {
				t.Fatalf("unexpected lyrics default: %+v, %v", options, err)
			}
			if doc.Config.Metadata.Automatic.Enabled {
				t.Fatal("lyrics import changed the global automatic policy")
			}
		})
	}
}

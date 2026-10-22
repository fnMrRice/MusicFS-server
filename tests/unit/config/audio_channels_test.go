package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAudioChannelConfigurationAndSchema(t *testing.T) {
	tool, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, _ := json.Marshal(filepath.ToSlash(tool))
	plugin := "plugins: [{pluginId: musicfs.plugin.decoder.ffmpeg, execution: builtin, enabled: true, instances: [{instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77, enabled: true, config: {ffmpegPath: " + string(path) + ", ffprobePath: " + string(path) + "}}]}]\n"
	for _, layout := range []string{"stereo", "5.1", "7.1", "mono", "5.1(side)", "9.1", ""} {
		t.Run(layout, func(t *testing.T) {
			raw := minimalYAML + plugin + "audioChannels: {enabled: true, outputLayout: '" + layout + "'}\n"
			valid := layout == "stereo" || layout == "5.1" || layout == "7.1"
			doc, err := parse(t, raw)
			if (err == nil) != valid {
				t.Fatalf("unexpected channel config validation: %v", err)
			}
			if valid && doc.Config.AudioChannels.OutputLayout != layout {
				t.Fatal("output layout changed during loading")
			}
			validateDSPConfigSchema(t, raw, valid)
		})
	}
	for _, configured := range []string{"", strings.ReplaceAll(plugin, "enabled: true", "enabled: false"), strings.Replace(plugin, "execution: builtin", "execution: process", 1)} {
		raw := minimalYAML + configured + "audioChannels: {enabled: true, outputLayout: '5.1'}\n"
		if _, err := parse(t, raw); err == nil {
			t.Fatal("surround output accepted without enabled builtin audio")
		}
		validateDSPConfigSchema(t, raw, false)
	}
	if _, err := parse(t, minimalYAML+"audioChannels: {outputLayout: stereo}\n"); err != nil {
		t.Fatalf("default stereo unexpectedly requires audio processing: %v", err)
	}
	for _, enabled := range []string{"", "enabled: false, "} {
		raw := minimalYAML + "audioChannels: {" + enabled + "outputLayout: '7.1'}\n"
		doc, err := parse(t, raw)
		if err != nil || doc.Config.AudioChannels.Enabled || doc.Config.AudioChannels.RequiresProcessing() {
			t.Fatalf("disabled historical layout required an audio processor: %v", err)
		}
		validateDSPConfigSchema(t, raw, true)
	}
}

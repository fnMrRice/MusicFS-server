package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestBuiltinDSPAliasesUseChainWithoutPluginRegistration(t *testing.T) {
	tool, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolPath, err := json.Marshal(filepath.ToSlash(tool))
	if err != nil {
		t.Fatal(err)
	}
	// 配置读取只检查工具文件存在，不执行该测试程序。
	plugin := "plugins: [{pluginId: musicfs.plugin.decoder.ffmpeg, execution: builtin, enabled: true, instances: [{instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77, enabled: true, config: {ffmpegPath: " + string(toolPath) + ", ffprobePath: " + string(toolPath) + "}}]}]\n"
	for _, name := range []string{"equalizer", "compressor", "reverb", "musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb"} {
		t.Run(name, func(t *testing.T) {
			raw := minimalYAML + plugin +
				"audioDSP: {chain: [{name: " + name + ", version: 1}]}\n"
			doc, err := parse(t, raw)
			if err != nil {
				t.Fatalf("builtin DSP required external registration: %v", err)
			}
			if len(doc.Config.AudioDSP.Plugins) != 0 || len(doc.Config.AudioDSP.Chain) != 1 || doc.Config.AudioDSP.Chain[0].Name != name {
				t.Fatal("builtin DSP startup chain changed during loading")
			}
			validateDSPConfigSchema(t, raw, true)
		})
	}
}

func TestBuiltinDSPNamesCannotBeOverriddenByExternalPlugins(t *testing.T) {
	for _, name := range []string{"gain", "bypass", "equalizer", "compressor", "reverb", "musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb"} {
		t.Run(name, func(t *testing.T) {
			raw := minimalYAML + "audioDSP: {plugins: [{name: " + name + ", executable: './dsp', sha256: '" + strings.Repeat("a", 64) + "'}]}\n"
			if _, err := parse(t, raw); err == nil {
				t.Fatal("external DSP shadowed a builtin name")
			}
			validateDSPConfigSchema(t, raw, false)
		})
	}
}

func validateDSPConfigSchema(t *testing.T, raw string, valid bool) {
	t.Helper()
	var value any
	if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, body)); (err == nil) != valid {
		t.Fatalf("DSP editor schema validity differs: %v", err)
	}
}

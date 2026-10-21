package config_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestFingerprintConfigurationDefaultsPathsAndSchema(t *testing.T) {
	for _, raw := range []string{minimalYAML, minimalYAML + "fingerprinting: {}\n", minimalYAML + "fingerprinting: {enabled: false}\n"} {
		doc, err := parse(t, raw)
		if err != nil {
			t.Fatal(err)
		}
		f := doc.Config.Fingerprinting
		if f.Enabled || f.FpcalcPath != "" || f.FpcalcSHA256 != "" || f.MaxConcurrent != 1 || f.TimeoutSeconds != 30 {
			t.Fatalf("wrong defaults: %+v", f)
		}
	}
	valid := minimalYAML + pluginYAML + "fingerprinting:\n  enabled: true\n  fpcalcPath: '../tools/fpcalc'\n  fpcalcSha256: '" + strings.Repeat("AB", 32) + "'\n  maxConcurrent: 2\n  timeoutSeconds: 120\n"
	doc, err := parse(t, valid)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(doc.Config.Fingerprinting.FpcalcPath) || filepath.Base(doc.Config.Fingerprinting.FpcalcPath) != "fpcalc" || doc.Config.Fingerprinting.FpcalcSHA256 != strings.Repeat("ab", 32) {
		t.Fatal("path or checksum normalization failed")
	}
	schema := configurationSchema(t)
	for name, tc := range map[string]struct {
		raw   string
		valid bool
	}{
		"valid": {valid, true}, "disabled": {minimalYAML + "fingerprinting: {}\n", true},
		"zero concurrent":         {strings.Replace(valid, "maxConcurrent: 2", "maxConcurrent: 0", 1), false},
		"large concurrent":        {strings.Replace(valid, "maxConcurrent: 2", "maxConcurrent: 3", 1), false},
		"zero timeout":            {strings.Replace(valid, "timeoutSeconds: 120", "timeoutSeconds: 0", 1), false},
		"large timeout":           {strings.Replace(valid, "timeoutSeconds: 120", "timeoutSeconds: 121", 1), false},
		"missing hash":            {strings.Replace(valid, "  fpcalcSha256: '"+strings.Repeat("AB", 32)+"'\n", "", 1), false},
		"bad hash":                {strings.Replace(valid, strings.Repeat("AB", 32), "PRIVATE-SENTINEL", 1), false},
		"empty path":              {strings.Replace(valid, "../tools/fpcalc", "", 1), false},
		"null":                    {minimalYAML + "fingerprinting: null\n", false},
		"unknown upload":          {minimalYAML + "fingerprinting: {upload: true}\n", false},
		"unknown algorithm":       {minimalYAML + "fingerprinting: {algorithm: 3}\n", false},
		"quoted concurrency":      {minimalYAML + "fingerprinting: {maxConcurrent: '1'}\n", false},
		"audio plugin disabled":   {strings.Replace(valid, "pluginId: musicfs.plugin.decoder.ffmpeg\n    executable: '../workers/audio'\n    enabled: true", "pluginId: musicfs.plugin.decoder.ffmpeg\n    executable: '../workers/audio'\n    enabled: false", 1), true},
		"audio instance disabled": {strings.Replace(valid, "instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77\n        enabled: true", "instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77\n        enabled: false", 1), true},
		"all disabled":            {strings.ReplaceAll(valid, "enabled: true", "enabled: false"), true},
		"missing audio":           {minimalYAML + "plugins: []\nfingerprinting: {enabled: true, fpcalcPath: 'fpcalc', fpcalcSha256: '" + strings.Repeat("a", 64) + "'}\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, tc.raw)
			if (err == nil) != tc.valid {
				t.Fatalf("loader validity: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
				t.Fatal("configuration error leaked input")
			}
			var value any
			if err := yaml.Unmarshal([]byte(tc.raw), &value); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(testkit.DecodeJSON(t, raw)); (err == nil) != tc.valid {
				t.Fatalf("schema validity: %v", err)
			}
		})
	}
}

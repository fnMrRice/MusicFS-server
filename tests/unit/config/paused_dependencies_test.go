package config_test

import (
	"strings"
	"testing"
)

func TestDisabledDependenciesKeepPersistentPolicyValid(t *testing.T) {
	combined := minimalYAML + pluginYAML + strings.TrimPrefix(scraperYAML, "plugins:\n")
	for name, raw := range map[string]string{
		"metadata-plugin":    strings.Replace(combined, "executable: '../workers/local-scraper'\n    enabled: true", "executable: '../workers/local-scraper'\n    enabled: false", 1) + "metadata:\n  automatic: {enabled: true, instancePriority: [scraper-local]}\n",
		"metadata-instance":  strings.Replace(combined, "role: scraper\n        enabled: true", "role: scraper\n        enabled: false", 1) + "metadata:\n  automatic: {enabled: true, instancePriority: [scraper-local]}\n",
		"writeback-plugin":   strings.Replace(combined, "enabled: true", "enabled: false", 1) + "writeBack: {enabled: true, roots: " + writeBackGrant + "}\n",
		"writeback-instance": strings.Replace(combined, "role: provider\n        enabled: true", "role: provider\n        enabled: false", 1) + "writeBack: {enabled: true, roots: " + writeBackGrant + "}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, raw); err != nil {
				t.Fatal("disabled dependency must survive restart", err)
			}
		})
	}
}

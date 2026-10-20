package config_test

import (
	"strings"
	"testing"
)

func TestDisabledDependenciesKeepPersistentPolicyValid(t *testing.T) {
	combined := minimalYAML + pluginYAML + strings.TrimPrefix(scraperYAML, "plugins:\n")
	for name, raw := range map[string]string{
		"metadata-plugin":    strings.Replace(combined, "executable: '../workers/local-scraper'\n    enabled: true", "executable: '../workers/local-scraper'\n    enabled: false", 1) + "metadata:\n  automatic: {enabled: true, instancePriority: [d248bf34-1780-59d3-a557-9cf5d889e977]}\n",
		"metadata-instance":  strings.Replace(combined, "name: 本地内容\n        enabled: true", "name: 本地内容\n        enabled: false", 1) + "metadata:\n  automatic: {enabled: true, instancePriority: [d248bf34-1780-59d3-a557-9cf5d889e977]}\n",
		"writeback-plugin":   strings.Replace(combined, "enabled: true", "enabled: false", 1) + "writeBack: {enabled: true, roots: " + writeBackGrant + "}\n",
		"writeback-instance": strings.Replace(combined, "instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n        enabled: true", "instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n        enabled: false", 1) + "writeBack: {enabled: true, roots: " + writeBackGrant + "}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, raw); err != nil {
				t.Fatal("disabled dependency must survive restart", err)
			}
		})
	}
}

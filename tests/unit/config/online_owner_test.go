package config_test

import (
	"strings"
	"testing"
)

func TestOnlineInstanceOwnerConfiguration(t *testing.T) {
	const onlineYAML = `plugins:
  - pluginId: musicfs.plugin.source.netease
    enabled: false
    instances:
      - instanceId: 3bdd4699-d687-43f4-a96f-1ff50d5b2cce
        config: {}
`
	for _, owner := range []string{"", "admin", "other-user"} {
		t.Run("owner_"+owner, func(t *testing.T) {
			raw := minimalYAML + onlineYAML
			if owner != "" {
				raw = strings.Replace(raw, "        config: {}", "        ownerUserId: "+owner+"\n        config: {}", 1)
			}
			doc, err := parse(t, raw)
			if err != nil {
				t.Fatal(err)
			}
			expected := owner
			if expected == "" {
				expected = "admin"
			}
			if actual := doc.Config.Plugins[0].Instances[0].OnlineOwnerUserID(); actual != expected {
				t.Fatalf("owner = %q, want %q", actual, expected)
			}
		})
	}
	for _, value := range []string{`''`, `' other-user'`, `'other-user '`, `"other\nuser"`, "12", "null", "'" + strings.Repeat("a", 129) + "'"} {
		raw := strings.Replace(minimalYAML+onlineYAML, "        config: {}", "        ownerUserId: "+value+"\n        config: {}", 1)
		if _, err := parse(t, raw); err == nil {
			t.Fatalf("invalid owner accepted: %q", value)
		}
	}
	raw := strings.Replace(minimalYAML+onlineYAML, "musicfs.plugin.source.netease", "musicfs.plugin.decoder.ncm", 1)
	raw = strings.Replace(raw, "        config: {}", "        ownerUserId: other-user\n        config: {}", 1)
	if _, err := parse(t, raw); err == nil {
		t.Fatal("shared plugin accepted an account owner")
	}
}

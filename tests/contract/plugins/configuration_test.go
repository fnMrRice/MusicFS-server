package plugins_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOfficialImportConfigurationIsBoundedAndExecutable(t *testing.T) {
	c := pluginconfig.LocalFiles()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	testkit.ValidatePlugin(t, "PluginConfiguration", c)
	if err := c.ValidateOptions(c.ImportDefaults); err != nil {
		t.Fatal(err)
	}
	options := c.ImportDefaults
	options.ImportLyrics = true
	if err := c.ValidateOptions(options); err != nil {
		t.Fatalf("lyrics import rejected: %v", err)
	}
	// The host evaluates the supplied rule, rather than a second hardcoded form.
	c.ImportOptionsSchema["properties"].(map[string]any)["maxDepth"].(map[string]any)["maximum"] = 4
	options = c.ImportDefaults
	options.MaxDepth = 5
	if c.ValidateOptions(options) == nil {
		t.Fatal("plugin schema maximum ignored")
	}
	options.MaxDepth = 4
	if err := c.ValidateOptions(options); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*pluginproto.Configuration){
		func(c *pluginproto.Configuration) { c.ImportOptionsSchema["$ref"] = "https://untrusted.invalid/schema" },
		func(c *pluginproto.Configuration) { c.ImportOptionsSchema["script"] = "throw new Error()" },
		func(c *pluginproto.Configuration) { c.ImportOptionsSchema["additionalProperties"] = true },
		func(c *pluginproto.Configuration) { c.ImportOptionsSchema["description"] = strings.Repeat("x", 1025) },
		func(c *pluginproto.Configuration) { c.SchemaRevision = "" },
		func(c *pluginproto.Configuration) {
			c.ImportOptionsSchema["properties"].(map[string]any)["secret"] = map[string]any{"type": "object", "$ref": "file:///private"}
		},
		func(c *pluginproto.Configuration) {
			child := c.ImportOptionsSchema
			for i := 0; i < 10; i++ {
				next := map[string]any{}
				child["if"] = next
				child = next
			}
		},
	} {
		invalid := pluginconfig.LocalFiles()
		mutate(invalid)
		if invalid.Validate() == nil {
			t.Fatal("invalid configuration declaration accepted")
		}
	}
}

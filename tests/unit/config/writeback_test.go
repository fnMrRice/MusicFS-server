package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

const writeBackGrant = "[{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: [lyrics, artwork]}]"

func TestWriteBackConfigurationStrictGrantsAndDefaults(t *testing.T) {
	doc, err := parse(t, minimalYAML)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.WriteBack.Enabled || doc.Config.WriteBack.AllowOverwrite || doc.Config.WriteBack.MaxFileBytes != 8<<30 || len(doc.Config.WriteBack.Roots) != 0 {
		t.Fatal("writeback defaults grant implicit writes")
	}
	schema := configurationSchema(t)
	for name, tc := range map[string]struct {
		value string
		valid bool
	}{
		"disabled":           {"{}", true},
		"enabled":            {"{enabled: true, roots: " + writeBackGrant + "}", true},
		"overwrite-explicit": {"{enabled: true, allowOverwrite: true, roots: " + writeBackGrant + "}", true},
		"disabled-grant":     {"{enabled: false, roots: " + writeBackGrant + "}", true},
		"minimum":            {"{maxFileBytes: 46}", true},
		"maximum":            {"{maxFileBytes: 1099511627776}", true},
		"zero":               {"{maxFileBytes: 0}", false},
		"too-small":          {"{maxFileBytes: 45}", false},
		"too-large":          {"{maxFileBytes: 1099511627777}", false},
		"no-grants":          {"{enabled: true}", false},
		"empty-grants":       {"{enabled: true, roots: []}", false},
		"null":               {"null", false},
		"null-grants":        {"{roots: null}", false},
		"unknown":            {"{privatePath: forbidden}", false},
		"bad-bool":           {"{enabled: 'true'}", false},
		"missing-fields":     {"{roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root}]}", false},
		"empty-fields":       {"{roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: []}]}", false},
		"duplicate-fields":   {"{roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: [lyrics,lyrics]}]}", false},
		"unsupported-fields": {"{roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: [title]}]}", false},
		"root-path":          {"{roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: [lyrics], path: forbidden}]}", false},
	} {
		t.Run(name, func(t *testing.T) {
			raw := minimalYAML + pluginYAML + "writeBack: " + tc.value + "\n"
			_, err := parse(t, raw)
			if (err == nil) != tc.valid {
				t.Fatalf("loader valid=%v: %v", tc.valid, err)
			}
			var value any
			if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(testkit.DecodeJSON(t, body)); (err == nil) != tc.valid {
				t.Fatalf("schema valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestWriteBackReferencesEnablementAndClone(t *testing.T) {
	base := minimalYAML + pluginYAML
	for name, raw := range map[string]string{
		"unknown-instance":  base + "writeBack: {roots: [{instanceId: missing, rootId: root, fields: [lyrics]}]}\n",
		"nonlocal-instance": base + "writeBack: {roots: [{instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77, rootId: root, fields: [lyrics]}]}\n",
		"unknown-root":      base + "writeBack: {roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: missing, fields: [lyrics]}]}\n",
		"duplicate-root":    base + "writeBack: {roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: [lyrics]}, {instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: root, fields: [artwork]}]}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, raw); err == nil {
				t.Fatal("invalid writeback dependency accepted")
			}
		})
	}
	if _, err := parse(t, strings.Replace(base, "enabled: true", "enabled: false", 1)+"writeBack: {enabled: false, roots: "+writeBackGrant+"}\n"); err != nil {
		t.Fatal("disabled policy still required active dependency", err)
	}
	path := filepath.Join(t.TempDir(), "writeback.yaml")
	if err := os.WriteFile(path, []byte(base+"writeBack: {roots: "+writeBackGrant+"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Current().WriteBack
	for _, c := range []config.Config{store.Current(), store.Snapshot().Config} {
		c.WriteBack.Roots[0].RootID = "mutated"
		c.WriteBack.Roots[0].Fields[0] = "mutated"
	}
	if !reflect.DeepEqual(store.Current().WriteBack, want) || !reflect.DeepEqual(store.Snapshot().Config.WriteBack, want) {
		t.Fatal("snapshot modified source write grants")
	}
}

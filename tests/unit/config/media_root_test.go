package config_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestMediaRootFollowsDeploymentDataDirectory(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "config", "server.yaml")
	for _, tc := range []struct {
		name, yaml, data, media string
		overrides               map[string]string
	}{
		{name: "default", data: filepath.Join(directory, "config", "data")},
		{name: "relative deployment", yaml: "dataRoot: ../deployment-data\n", data: filepath.Join(directory, "deployment-data")},
		{name: "absolute deployment", data: filepath.Join(directory, "user-data"), overrides: map[string]string{"DATA_ROOT": filepath.Join(directory, "user-data")}},
		{name: "explicit media", yaml: "dataRoot: ../deployment-data\nmediaRoot: ../custom-media\n", data: filepath.Join(directory, "deployment-data"), media: filepath.Join(directory, "custom-media")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			overrides := map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}
			for key, value := range tc.overrides {
				overrides[key] = value
			}
			doc, err := config.ParseDocument([]byte(minimalYAML+tc.yaml), file, overrides)
			if err != nil {
				t.Fatal(err)
			}
			media := tc.media
			if media == "" {
				media = filepath.Join(tc.data, "media")
			}
			if doc.Config.DataRoot != tc.data || doc.Config.MediaRoot != media {
				t.Fatalf("media root does not follow deployment data: %s / %s", doc.Config.DataRoot, doc.Config.MediaRoot)
			}
		})
	}
}

func TestMediaRootRejectsUnsafeWritableOverlap(t *testing.T) {
	for _, raw := range []string{
		"mediaRoot: ''\n",
		"dataRoot: data\nmediaRoot: data\n",
		"dataRoot: data/child\nmediaRoot: data\n",
		"dataRoot: data\nmediaRoot: data/media\ncacheRoot: data/media/cache\n",
	} {
		if _, err := parse(t, minimalYAML+raw); err == nil {
			t.Fatalf("unsafe writable overlap accepted: %s", raw)
		}
	}
}

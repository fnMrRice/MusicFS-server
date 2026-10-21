package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestAllowedDirectoryRejectsJunctionEscape(t *testing.T) {
	allowed, outside := t.TempDir(), t.TempDir()
	junction := filepath.Join(allowed, "escape-junction")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	instance := config.Instance{Permissions: &config.Permissions{ReadDirectories: []string{allowed}}}
	if root, err := config.OpenAllowedDirectory(instance, junction); err == nil {
		root.Close()
		t.Fatal("junction escaped deployment permissions")
	}
}

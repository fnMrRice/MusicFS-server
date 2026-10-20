package config_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
)

func TestSnapshotFormattingDoesNotExposeStartupCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte(minimalYAML), 0600); err != nil {
		t.Fatal(err)
	}
	const secret = "private-startup-admin-credential-123456789"
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": secret}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	if snapshot.Config.SourceOverrides != nil || strings.Contains(fmt.Sprintf("%+v %#v", snapshot, snapshot), secret) {
		t.Fatal("formatted management snapshot exposed a startup credential")
	}
	if store.Current().SourceOverrides["ADMIN_TOKEN"] != secret {
		t.Fatal("redaction changed trusted startup configuration")
	}
}

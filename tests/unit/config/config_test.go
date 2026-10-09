package config_test

import (
	"github.com/fnMrRice/MusicFS-server/src/config"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPrecedenceAndBoundaries(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "配置.json")
	if err := os.WriteFile(file, []byte(`{"allowLoopbackHttp":true,"allowedOrigins":["http://127.0.0.1:8080"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSICFS_ADMIN_TOKEN", "a-long-test-only-credential")
	t.Setenv("MUSICFS_LISTEN", "127.0.0.1:9000")
	c, err := config.Load(file, map[string]string{"LISTEN": "127.0.0.1:8000"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8000" || c.DataRoot != filepath.Join(dir, "data") {
		t.Fatal("precedence or relative path resolution failed")
	}
	c.Listen = "0.0.0.0:8000"
	if c.Validate() == nil {
		t.Fatal("public plaintext accepted")
	}
	c.Listen = "127.0.0.1:8000"
	c.MusicRoots = []config.MusicRoot{{ID: "root", Name: "Music", Path: c.CacheRoot}}
	if c.Validate() == nil {
		t.Fatal("overlap accepted")
	}
	if err := os.WriteFile(file, []byte(`{"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(file, nil); err == nil {
		t.Fatal("unknown field accepted")
	}
}

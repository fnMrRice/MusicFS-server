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

func TestNCMWorkerConfiguration(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	content := `{"allowLoopbackHttp":true,"allowedOrigins":["http://127.0.0.1:8080"],"ncmWorkerPath":"worker/ncm","ncmWorkerSha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSICFS_ADMIN_TOKEN", "a-long-test-only-credential")
	c, err := config.Load(file, nil)
	if err != nil || c.NCMWorkerPath != filepath.Join(dir, "worker", "ncm") {
		t.Fatalf("NCM configuration failed: %v", err)
	}
	c.NCMWorkerSHA256 = "invalid"
	if c.Validate() == nil {
		t.Fatal("invalid NCM checksum accepted")
	}
}

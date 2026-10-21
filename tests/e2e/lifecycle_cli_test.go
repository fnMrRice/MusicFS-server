package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
)

func TestLifecycleActualCLIExitsAfterLastLease(t *testing.T) {
	executable := build(t, "musicfs")
	cfg, base := lifecycleConfig(t)
	path := writeYAMLConfiguration(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "serve", "--config", path, "--startup-mode", "client-managed")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(env), "MUSICFS_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "MUSICFS_ADMIN_TOKEN="+cfg.AdminToken)
	log, err := os.Create(filepath.Join(t.TempDir(), "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	t.Cleanup(func() {
		cancel()
		if !exited {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("owned CLI process did not stop")
			}
		}
	})
	for deadline := time.Now().Add(15 * time.Second); ; {
		response, e := http.Get(base + "/api/v1/health/ready")
		if e == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		select {
		case e := <-done:
			exited = true
			t.Fatalf("CLI startup failed: %v", e)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("CLI startup timed out")
		}
		time.Sleep(15 * time.Millisecond)
	}
	c := login(t, base, cfg.AdminToken)
	status := lifecycleStatus(t, c)
	if status.StartupMode != lifecycle.ClientManaged || status.LeaseTTLMillis != 30000 {
		t.Fatal("CLI did not fix managed startup and default lease duration")
	}
	lease := acquireLifecycle(t, c, status.InstanceID, "client-real-cli")
	code, raw, _ := c.request(t, "POST", "/api/v1/lifecycle/leases/"+lease.ID+"/release", lifecycle.ReleaseRequest{InstanceID: status.InstanceID}, "")
	if code != 200 {
		t.Fatalf("CLI release: %d %s", code, raw)
	}
	select {
	case err = <-done:
		exited = true
		if err != nil {
			t.Fatal("managed CLI exited unsuccessfully", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("managed CLI stayed resident after last release")
	}
	data, err := os.ReadFile(filepath.Join(cfg.DataRoot, "server.lock"))
	var record bootstrap.StartupRecord
	if err != nil || json.Unmarshal(data, &record) != nil || record.PID != cmd.Process.Pid || record.InstanceID != status.InstanceID {
		t.Fatal("persisted startup record did not identify the actual child process", err)
	}
}

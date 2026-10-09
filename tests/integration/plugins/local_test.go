package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func wave(t *testing.T, path string) {
	t.Helper()
	b := make([]byte, 44+4800*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 96000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func buildWorker(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worker")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", path, "./src/app/musicfs-plugin-local")
	cmd.Dir = filepath.Join("..", "..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return path
}

func TestRealWorkerScanRestartRenameResolveAndIsolation(t *testing.T) {
	executable := buildWorker(t)
	b, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	root := t.TempDir()
	wave(t, filepath.Join(root, "中文.wav"))
	wave(t, filepath.Join(root, "copy.wav"))
	cfg := pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sum), InstanceID: "local-a", UserID: "owner", Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}, StateDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w, err := pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	page, err := w.Scan(ctx, pluginproto.ScanRequest{RootID: "music", ScanID: "s1", Generation: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" || page.Complete {
		t.Fatalf("unexpected first page: %+v", page)
	}
	last, err := w.Scan(ctx, pluginproto.ScanRequest{RootID: "music", ScanID: "s1", Generation: 1, Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if !last.Complete || len(last.Items) != 1 || page.Items[0].ExternalID == last.Items[0].ExternalID {
		t.Fatalf("bad final page: %+v", last)
	}
	original := last.Items[0]
	resource, err := w.Resolve(ctx, pluginproto.ResolveRequest{ExternalID: original.ExternalID, ExpectedVersion: original.ObservedVersion})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Access.Open(ctx, sourceaccess.Scope{UserID: "other", InstanceID: "local-a", TaskID: "provider-resolve"}, resource.ResourceRef); err == nil {
		t.Fatal("cross-user resource accepted")
	}
	handle, err := w.Access.Open(ctx, sourceaccess.Scope{UserID: "owner", InstanceID: "local-a", TaskID: "provider-resolve"}, resource.ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = handle.Read(make([]byte, 44)); err != nil {
		t.Fatal("worker shutdown invalidated independent open source", err)
	}
	handle.Close()
	if err = os.Rename(filepath.Join(root, original.RelativePath), filepath.Join(root, "renamed.wav")); err != nil {
		t.Fatal(err)
	}
	w, err = pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	again, err := w.Scan(ctx, pluginproto.ScanRequest{RootID: "music", ScanID: "s2", Generation: 2, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range again.Items {
		if item.RelativePath == "renamed.wav" {
			found = item.ExternalID == original.ExternalID
		}
	}
	if !found {
		t.Fatal("restarted renamed source lost stable external ID")
	}
	if _, err = w.Resolve(ctx, pluginproto.ResolveRequest{ExternalID: original.ExternalID, ExpectedVersion: "wrong"}); err == nil {
		t.Fatal("wrong version accepted")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err = w.Scan(canceled, pluginproto.ScanRequest{RootID: "music", ScanID: "cancel", Generation: 3, Limit: 200}); err == nil {
		t.Fatal("canceled scan succeeded")
	}
}

func TestWorkerRegistryRejectsUntrustedBinary(t *testing.T) {
	cfg := pluginhost.Config{Executable: os.Args[0], SHA256: "wrong", InstanceID: "a", UserID: "u", StateDir: t.TempDir()}
	if cfg.Validate() == nil {
		t.Fatal("wrong checksum accepted")
	}
	cfg.Disabled = true
	if cfg.Validate() == nil {
		t.Fatal("disabled worker accepted")
	}
}

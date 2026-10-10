//go:build windows || linux

package plugins_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func stableSource(t *testing.T, data []byte) (*sourceaccess.Access, *sourceaccess.Handle, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "source.ncm")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	scope := sourceaccess.Scope{UserID: "owner", InstanceID: "local", TaskID: "hash", AssetID: "asset"}
	r, err := a.Register(scope, "root", "source.ncm", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.Open(context.Background(), scope, r.ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(); a.Revoke(scope.TaskID) })
	return a, h, path
}

func TestStableOriginalBytesEmptyFileAndIndependentHandle(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("CTENFDAM-original-container-bytes")} {
		_, h, _ := stableSource(t, data)
		s, err := h.Stable(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		h.Close()
		got := make([]byte, len(data)+1)
		n, err := s.ReadAt(got, 0)
		if n != len(data) || !errors.Is(err, io.EOF) || !bytes.Equal(got[:n], data) {
			t.Fatalf("original bytes differ: n=%d err=%v", n, err)
		}
		published := false
		if err := s.Publish(func() error { published = true; return nil }); err != nil || !published {
			t.Fatalf("stable publication failed: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReadAt(got, 0); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("closed reader accepted: %v", err)
		}
		if err := s.Publish(func() error { t.Fatal("closed reader published"); return nil }); err == nil {
			t.Fatal("closed reader published")
		}
	}
}

func TestStableCancellationClosesProtection(t *testing.T) {
	_, h, path := stableSource(t, []byte("original"))
	ctx, cancel := context.WithCancel(context.Background())
	s, err := h.Stable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(s.Validate(), context.Canceled) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !errors.Is(s.Validate(), context.Canceled) {
		t.Fatal("cancelled reader stayed valid")
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("cancelled protection still blocks writing: %v", err)
	}
	f.Close()
}

func TestSourceVersionDetectsRestoredMtimeAndSameLengthWrite(t *testing.T) {
	a, h, path := stableSource(t, []byte("before"))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Ensure even coarse file-change clocks advance independently of mtime.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(path, []byte("after!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	current, err := a.Inspect("root", "source.ncm")
	if err != nil {
		t.Fatal(err)
	}
	if current.ObservedVersion == h.Entry.ObservedVersion || current.FileIdentity != h.Entry.FileIdentity {
		t.Fatal("same-length rewrite with restored mtime did not change version")
	}
	if _, err := h.Stable(context.Background()); err == nil {
		t.Fatal("stale version gained stable protection")
	}
}

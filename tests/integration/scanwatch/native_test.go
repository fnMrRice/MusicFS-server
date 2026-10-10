//go:build windows || linux

package scanwatch_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/scanwatch"
)

func openDirectory(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := testOpenHandle(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func hintChannel() (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	return ch, func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func awaitHint(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("directory change was not observed")
	}
}

func noHint(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("unexpected directory notification")
	case <-time.After(350 * time.Millisecond):
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("synthetic notification fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeDirectoryChangesAreNonrecursiveAndCloseJoins(t *testing.T) {
	if runtime.GOOS == "linux" {
		assertNativeNonrecursive(t)
	} else {
		t.Log("Windows may report direct-child metadata changes; no descendant-hint absence assertion")
	}
	// A separate empty root prevents an earlier child metadata hint from
	// satisfying the positive assertion for a directly created file.
	root := t.TempDir()
	f := openDirectory(t, root)
	hints, hint := hintChannel()
	var failures atomic.Int32
	stop, err := scanwatch.StartNative(context.Background(), []*os.File{f}, hint, func() { failures.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "direct.txt"))
	awaitHint(t, hints)
	// Multiple callers may join shutdown. No callback may outlive it.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(stop)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("native shutdown did not complete")
	}
	for len(hints) > 0 {
		<-hints
	}
	writeFile(t, filepath.Join(root, "after-close.txt"))
	noHint(t, hints)
	if failures.Load() != 0 {
		t.Fatal("normal shutdown reported a notification failure")
	}
}

func assertNativeNonrecursive(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	deep := filepath.Join(root, "child", "deeper")
	if err := os.MkdirAll(deep, 0700); err != nil {
		t.Fatal(err)
	}
	f := openDirectory(t, root)
	hints, hint := hintChannel()
	var failures atomic.Int32
	stop, err := scanwatch.StartNative(context.Background(), []*os.File{f}, hint, func() { failures.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	_ = f.Close()
	writeFile(t, filepath.Join(deep, "nested.txt"))
	noHint(t, hints)
	stop()
	if failures.Load() != 0 {
		t.Fatal("nonrecursive watch reported a notification failure")
	}
}

func TestNativeDirectoryBindingDoesNotReopenOriginalPath(t *testing.T) {
	parent := t.TempDir()
	original, moved := filepath.Join(parent, "original"), filepath.Join(parent, "moved")
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	f := openDirectory(t, original)
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0700); err != nil {
		t.Fatal(err)
	}
	hints, hint := hintChannel()
	stop, err := scanwatch.StartNative(context.Background(), []*os.File{f}, hint, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	_ = f.Close()
	writeFile(t, filepath.Join(original, "replacement.txt"))
	noHint(t, hints)
	writeFile(t, filepath.Join(moved, "bound.txt"))
	awaitHint(t, hints)
}

func TestNativeCancellationAndDirectoryLimit(t *testing.T) {
	f := openDirectory(t, t.TempDir())
	dirs := make([]*os.File, scanwatch.MaxDirectories)
	for i := range dirs {
		dirs[i] = f
	}
	ctx, cancel := context.WithCancel(context.Background())
	var failures atomic.Int32
	stop, err := scanwatch.StartNative(ctx, dirs, func() {}, func() { failures.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	cancel()
	started := time.Now()
	stop()
	if time.Since(started) > 3*time.Second || failures.Load() != 0 {
		t.Fatal("cancellation did not close cleanly")
	}
	_, err = scanwatch.StartNative(context.Background(), append(dirs, f), func() {}, func() {})
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "INVALID_ARGUMENT" {
		t.Fatalf("expected directory limit rejection, got %v", err)
	}
}

func TestNativeRejectsInvalidHandlesAndCancelledRegistration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	writeFile(t, path)
	f := openDirectory(t, path)
	closed := openDirectory(t, root)
	_ = closed.Close()
	for _, dirs := range [][]*os.File{nil, {nil}, {f}, {closed}} {
		stop, err := scanwatch.StartNative(context.Background(), dirs, func() {}, func() {})
		if err == nil || stop != nil {
			t.Fatal("invalid directory handle was accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := scanwatch.StartNative(ctx, []*os.File{openDirectory(t, root)}, func() {}, func() {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestNativeRemovedDirectoryFailsOnceForPeriodicFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "watched")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	f := openDirectory(t, root)
	failed := make(chan struct{}, 4)
	stop, err := scanwatch.StartNative(context.Background(), []*os.File{f}, func() {}, func() { failed <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	_ = f.Close()
	if err = os.Remove(root); err != nil {
		t.Fatal(err)
	}
	select {
	case <-failed:
	case <-time.After(3 * time.Second):
		t.Fatal("lost directory did not signal periodic fallback")
	}
	stop()
	if len(failed) != 0 {
		t.Fatal("notification failure was reported more than once")
	}
}

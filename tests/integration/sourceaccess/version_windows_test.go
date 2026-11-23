package sourceaccess_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsObservedVersionIsContentHash(t *testing.T) {
	root := t.TempDir()
	const name = "track.wav"
	data := packageWave()
	packageWrite(t, root, name, data)
	access := packageAccess(t, root)
	entry, err := access.Inspect("music", name)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("%x", sha256.Sum256(data)); entry.ObservedVersion != want {
		t.Fatalf("source version = %s, want content hash %s", entry.ObservedVersion, want)
	}

	// 改变只读属性和最后写入时间，保留扫描时的音频字节。
	path := filepath.Join(root, name)
	timestamp := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, timestamp, timestamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })

	after, err := access.Inspect("music", name)
	if err != nil || after.ObservedVersion != entry.ObservedVersion {
		t.Fatalf("unchanged audio has a different version: %s, %v", after.ObservedVersion, err)
	}
}

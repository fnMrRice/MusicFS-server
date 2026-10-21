package sourceaccess_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestDirectoryBoundaryRejectsSiblingAndEscapingLink(t *testing.T) {
	parent := t.TempDir()
	inside, outside := filepath.Join(parent, "music"), t.TempDir()
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: inside, Boundary: parent}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "track.wav"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Inspect("music", "track.wav"); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "outside", Path: outside, Boundary: parent}}); err == nil {
		t.Fatal("accepted a directory outside deployment boundary")
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if _, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "escape", Path: link, Boundary: parent}}); err == nil {
		t.Fatal("followed a link outside deployment boundary")
	}
}

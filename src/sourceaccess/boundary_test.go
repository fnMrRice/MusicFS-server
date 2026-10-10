package sourceaccess

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryBoundaryRejectsSiblingAndEscapingLink(t *testing.T) {
	parent := t.TempDir()
	inside, outside := filepath.Join(parent, "music"), t.TempDir()
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	access, err := New([]RootConfig{{ID: "music", Path: inside, Boundary: parent}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := access.openRoot("music")
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if _, err := New([]RootConfig{{ID: "outside", Path: outside, Boundary: parent}}); err == nil {
		t.Fatal("accepted a directory outside deployment boundary")
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	if _, err := New([]RootConfig{{ID: "escape", Path: link, Boundary: parent}}); err == nil {
		t.Fatal("followed a link outside deployment boundary")
	}
}

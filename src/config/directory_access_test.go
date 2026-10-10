package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDirectoryPathWindowsAllowsExplicitUNC(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path syntax")
	}
	for _, path := range []string{`C:\Music`, `C:/Music`, `\\server\share\Music`, `\\nas.example\shared music`} {
		if err := ValidateDirectoryPath(path); err != nil {
			t.Fatal("valid path syntax rejected", path, err)
		}
	}
}

func TestAllowedDirectoryRejectsSymlinkEscapeAndHoldsBoundary(t *testing.T) {
	allowed, outside := t.TempDir(), t.TempDir()
	inside := filepath.Join(allowed, "inside")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	instance := Instance{Permissions: &Permissions{ReadDirectories: []string{allowed}}}
	root, err := OpenAllowedDirectory(instance, inside)
	if err != nil {
		t.Fatal(err)
	}
	root.Close()
	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("directory symlink creation unavailable: %v", err)
	}
	if root, err := OpenAllowedDirectory(instance, link); err == nil {
		root.Close()
		t.Fatal("symlink escaped deployment permissions")
	}
	// An opened root keeps its directory handle if a path is subsequently replaced.
	root, err = OpenAllowedDirectory(instance, inside)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(inside, filepath.Join(allowed, "original")); err != nil {
		t.Skipf("open directory rename unavailable: %v", err)
	}
	if err := os.Symlink(outside, inside); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if file, err := root.Open("secret"); err == nil {
		file.Close()
		t.Fatal("opened directory followed replacement link")
	}
}

func TestDirectoryBoundaryUsesNearestAllowedRoot(t *testing.T) {
	parent := t.TempDir()
	nested := filepath.Join(parent, "nested")
	i := Instance{Permissions: &Permissions{ReadDirectories: []string{parent, nested}}}
	boundary, relative, err := DirectoryBoundary(i, nested)
	if err != nil || boundary != nested || relative != "." {
		t.Fatal("nested allowed root had a parent beyond its boundary", boundary, relative, err)
	}
}

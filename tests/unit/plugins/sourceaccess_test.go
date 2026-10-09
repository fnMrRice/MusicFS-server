package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func TestResourceScopeVersionsRevocationAndTraversal(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.wav")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "r", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../secret", filepath.Join(root, "file.wav"), "."} {
		if _, err := a.Inspect("r", path); err == nil {
			t.Fatalf("accepted unsafe path %s", path)
		}
	}
	scope := sourceaccess.Scope{UserID: "u", InstanceID: "i", TaskID: "t", AssetID: "a"}
	r, err := a.Register(scope, "r", "file.wav", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	other := scope
	other.InstanceID = "other"
	if _, err = a.Open(context.Background(), other, r.ResourceRef); err == nil {
		t.Fatal("cross-instance reference accepted")
	}
	f, err := a.Open(context.Background(), scope, r.ResourceRef)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("changed size"), 0600); err != nil {
		t.Fatal(err)
	}
	if f.Validate() == nil {
		t.Fatal("open source mutation not detected")
	}
	f.Close()
	if _, err = a.Open(context.Background(), scope, r.ResourceRef); err == nil {
		t.Fatal("changed source reopened")
	}
	a.Revoke("t")
	if _, err = a.Open(context.Background(), scope, r.ResourceRef); err == nil {
		t.Fatal("revoked reference accepted")
	}
	if _, err = sourceaccess.New([]sourceaccess.RootConfig{{ID: "r", Path: root, ExpectedIdentity: "other"}}); err == nil {
		t.Fatal("changed root identity accepted")
	}
}

func TestSourceRootRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.wav"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlinks unavailable: " + err.Error())
	}
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "r", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Inspect("r", "escape/secret.wav"); err == nil {
		t.Fatal("symlink escaped authorized root")
	}
}

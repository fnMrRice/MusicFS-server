package plugins_test

import (
	"context"
	"crypto/sha512"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestSidecarGrantsAreExactVersionedAndReadOnly(t *testing.T) {
	root := t.TempDir()
	contents := map[string][]byte{"track.mp3": []byte("ID3-test-audio"), "track.lrc": []byte("[00:01]中文"), "track.txt": []byte("translation"), "other.lrc": []byte("not authorized"), "cover.jpg": []byte("picture")}
	for name, body := range contents {
		if err := os.WriteFile(filepath.Join(root, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := a.Inspect("root", "track.mp3")
	if err != nil {
		t.Fatal(err)
	}
	scope := sourceaccess.Scope{UserID: "owner", InstanceID: "scraper", TaskID: "task", AssetID: "asset"}
	grants, err := a.AuthorizeSidecars(context.Background(), scope, "root", "track.mp3", entry.ObservedVersion, []string{"lyrics"})
	if err != nil || len(grants) != 2 {
		t.Fatalf("exact sibling grants: %d %v", len(grants), err)
	}
	for _, grant := range grants {
		if grant.Kind != "lyrics" || grant.Match != "exactStem" || grant.Resource.RelativePath == "other.lrc" {
			t.Fatal("grant escaped exact selected siblings")
		}
		wrong := scope
		wrong.AssetID = "other"
		if _, err = a.Open(context.Background(), wrong, grant.Resource.ResourceRef); err == nil {
			t.Fatal("grant crossed asset scope")
		}
		h, err := a.Open(context.Background(), scope, grant.Resource.ResourceRef)
		if err != nil {
			t.Fatal(err)
		}
		h.Close()
	}
	for name, before := range contents {
		after, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || sha512.Sum512(before) != sha512.Sum512(after) {
			t.Fatal("source bytes changed")
		}
	}
	if _, err = a.AuthorizeSidecars(context.Background(), scope, "root", "../track.mp3", entry.ObservedVersion, []string{"lyrics"}); err == nil {
		t.Fatal("relative path escape accepted")
	}
	if err = os.WriteFile(filepath.Join(root, "track.mp3"), []byte("replaced-audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.AuthorizeSidecars(context.Background(), scope, "root", "track.mp3", entry.ObservedVersion, []string{"lyrics"}); err == nil {
		t.Fatal("stale source authorized new sidecars")
	}
}

func TestSidecarSymlinksAreNotGranted(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "track.mp3"), []byte("ID3-test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "track.lrc")); err != nil {
		t.Skip("host cannot create a symlink fixture")
	}
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := a.Inspect("root", "track.mp3")
	if err != nil {
		t.Fatal(err)
	}
	grants, err := a.AuthorizeSidecars(context.Background(), sourceaccess.Scope{UserID: "owner", InstanceID: "scraper", TaskID: "task", AssetID: "asset"}, "root", "track.mp3", entry.ObservedVersion, []string{"lyrics"})
	if err != nil || len(grants) != 0 {
		t.Fatalf("linked lyrics authorized: %d %v", len(grants), err)
	}
}

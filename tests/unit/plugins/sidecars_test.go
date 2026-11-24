package plugins_test

import (
	"context"
	"crypto/sha512"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestScrapeFilesAreExactVersionedAndReadOnly(t *testing.T) {
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
	files, err := a.OpenScrapeFiles(context.Background(), scope, "root", "track.mp3", entry.ObservedVersion, entry.SizeBytes, []string{"lyrics"})
	if err != nil || len(files) != 3 {
		t.Fatalf("exact protected inputs: %d %v", len(files), err)
	}
	expected := []string{"track.mp3", "track.lrc", "track.txt"}
	for i, file := range files {
		if file.Issue != "" || file.Reader == nil || file.Version == "" {
			t.Fatalf("unavailable protected input: %+v", file)
		}
		defer file.Reader.Close()
		if i > 0 && (file.Kind != "lyrics" || file.Match != "exactStem") {
			t.Fatal("authorized an unselected sibling")
		}
		body, err := io.ReadAll(io.NewSectionReader(file.Reader, 0, file.SizeBytes))
		if err != nil || string(body) != string(contents[expected[i]]) {
			t.Fatalf("unexpected protected input: %q %v", body, err)
		}
	}
	for _, file := range files {
		if err := file.Reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for name, before := range contents {
		after, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || sha512.Sum512(before) != sha512.Sum512(after) {
			t.Fatal("source bytes changed")
		}
	}
	if _, err = a.OpenScrapeFiles(context.Background(), scope, "root", "../track.mp3", entry.ObservedVersion, entry.SizeBytes, []string{"lyrics"}); err == nil {
		t.Fatal("relative path escape accepted")
	}
	if err = os.WriteFile(filepath.Join(root, "track.mp3"), []byte("new-test-audio"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.OpenScrapeFiles(context.Background(), scope, "root", "track.mp3", entry.ObservedVersion, entry.SizeBytes, []string{"lyrics"}); err == nil {
		t.Fatal("stale source authorized new sidecars")
	}
}

func TestScrapeFilesExcludeLinkedSidecars(t *testing.T) {
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
	files, err := a.OpenScrapeFiles(context.Background(), sourceaccess.Scope{UserID: "owner", InstanceID: "scraper", TaskID: "task", AssetID: "asset"}, "root", "track.mp3", entry.ObservedVersion, entry.SizeBytes, []string{"lyrics"})
	if err != nil || len(files) != 1 || files[0].Kind != "audio" {
		t.Fatalf("linked lyrics authorized: %d %v", len(files), err)
	}
	files[0].Reader.Close()
}

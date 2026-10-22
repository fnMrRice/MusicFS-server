package contenttype_test

import (
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"reflect"
	"testing"
)

func TestDirectoryPackageOrderPreservesVersionsAndNumericTracks(t *testing.T) {
	paths := []string{"WAV/10.wav", "MP3/2.mp3", "WAV/2.wav", "MP3", ".", "WAV", "特典", "特典/2", "特典/02x", "空目录"}
	entries := make([]contenttype.PackageEntry, len(paths))
	for i, path := range paths {
		entries[i] = contenttype.PackageEntry{Path: path}
	}
	ordered, err := (contenttype.DirectoryOrganizer{}).OrderPackage(entries)
	want := []string{".", "MP3", "MP3/2.mp3", "WAV", "WAV/2.wav", "WAV/10.wav", "特典", "特典/2", "特典/02x", "空目录"}
	if err != nil || !reflect.DeepEqual(ordered, want) {
		t.Fatalf("Package structure or order changed: %v %v", ordered, err)
	}
	if entries[0].Path != paths[0] {
		t.Fatal("Organizer modified source entries")
	}
}

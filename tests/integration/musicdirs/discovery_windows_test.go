package musicdirs_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/musicdirs"
)

func TestLogicalDriveRoots(t *testing.T) {
	if got := musicdirs.DriveRoots(0); len(got) != 0 {
		t.Fatalf("empty drive mask produced roots: %v", got)
	}
	want := []string{"A:\\", "C:\\", "Z:\\"}
	if got := musicdirs.DriveRoots(1 | 1<<2 | 1<<25 | 1<<31); !reflect.DeepEqual(got, want) {
		t.Fatalf("drive roots = %v, want %v", got, want)
	}
}

func TestFindMusicDirectoriesDeduplicatesWindowsPathCase(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Music")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	roots := []string{root, strings.ToUpper(root), strings.ToLower(root)}
	if got := musicdirs.Find(roots); !reflect.DeepEqual(got, []string{path}) {
		t.Fatalf("discovered directories = %v, want one case-insensitive directory", got)
	}
}

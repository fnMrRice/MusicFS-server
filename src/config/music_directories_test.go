package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDefaultUserMusicDirectoryDoesNotRequireExistingDirectory(t *testing.T) {
	home := t.TempDir()
	setMusicTestHome(t, home)
	want := filepath.Join(home, "Music")
	if got := defaultUserMusicDirectory(); got != want || !filepath.IsAbs(got) {
		t.Fatalf("default music directory = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("default lookup must not create a directory: %v", err)
	}
}

func TestDefaultUserMusicDirectoryWithoutHome(t *testing.T) {
	setMusicTestHome(t, "")
	if got := defaultUserMusicDirectory(); got != "" {
		t.Fatalf("default music directory without home = %q, want empty", got)
	}
}

func setMusicTestHome(t *testing.T, home string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	} else {
		t.Setenv("HOME", home)
	}
}

func TestFindMusicDirectoriesChecksKnownImmediateChildren(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	want := []string{}
	for _, name := range []string{"Music", "NeteaseMusic", "CloudMusic", "QQMusic", "Kugou", "KuwoMusic"} {
		path := filepath.Join(first, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		want = append(want, path)
	}
	// An empty directory is valid; contents are not required to be audio files.
	if err := os.WriteFile(filepath.Join(first, "NeteaseMusic", "readme.txt"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(second, "Downloads", "Music"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(second, "QQMusic"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "Music"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	want = append(want, filepath.Join(second, "QQMusic"))
	roots := []string{first, second, filepath.Join(first, "missing"), first}
	if got := findMusicDirectories(roots); !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered directories = %v, want %v", got, want)
	}
}

func TestFindMusicDirectoriesDeduplicatesSymlinks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Music")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "NeteaseMusic")); err != nil {
		t.Skipf("cannot create directory symlink: %v", err)
	}
	if got := findMusicDirectories([]string{root}); !reflect.DeepEqual(got, []string{path}) {
		t.Fatalf("discovered directories = %v, want one canonical directory", got)
	}
}

func TestFindMusicDirectoriesSkipsUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits do not restrict directory reads on Windows")
	}
	root := t.TempDir()
	path := filepath.Join(root, "Music")
	if err := os.Mkdir(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
	if f, err := os.Open(path); err == nil {
		f.Close()
		t.Skip("process can read directories without permission bits")
	}
	if got := findMusicDirectories([]string{root}); len(got) != 0 {
		t.Fatalf("discovered unreadable directories: %v", got)
	}
}

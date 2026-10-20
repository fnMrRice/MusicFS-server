//go:build windows

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"golang.org/x/sys/windows"
)

func TestWindowsHiddenAttributesFilterFilesDirectoriesAndPreserveExplicitRoot(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"normal.wav", "hidden.wav", "secret/song.wav"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("audio"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{root, filepath.Join(root, "hidden.wav"), filepath.Join(root, "secret")} {
		name, err := syscall.UTF16PtrFromString(file)
		if err != nil {
			t.Fatal(err)
		}
		attributes, err := syscall.GetFileAttributes(name)
		if err != nil || syscall.SetFileAttributes(name, attributes|syscall.FILE_ATTRIBUTE_HIDDEN) != nil {
			t.Fatal("cannot set real Windows hidden attribute", err)
		}
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, SkipHidden: true}
	names, excluded := filteredNames(t, access, scope)
	sort.Strings(excluded)
	if !reflect.DeepEqual(names, []string{"normal.wav"}) || !reflect.DeepEqual(excluded, []string{"hidden.wav", "secret"}) {
		t.Fatal("Windows hidden attributes were not filtered", names, excluded)
	}
	dirs, err := access.WatchDirectories(context.Background(), scope, 256)
	for _, dir := range dirs {
		dir.File.Close()
	}
	if err != nil || len(dirs) != 1 {
		t.Fatal("watch entered Windows hidden directory", err)
	}
	scope.SkipHidden = false
	names, _ = filteredNames(t, access, scope)
	if len(names) != 3 {
		t.Fatal("hidden attribute override failed", names)
	}
}

func TestFlatScanDoesNotInspectLockedChildDirectoryMarkers(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "unselected")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "normal.wav"), []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(child)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal("cannot lock unselected child directory", err)
	}
	defer windows.CloseHandle(handle)
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, SkipHidden: true, RespectNoMedia: true}
	entries, _, issues, _, err := access.EntriesWithExclusions(context.Background(), scope)
	if err != nil || len(issues) != 0 || len(entries) != 1 {
		t.Fatal("flat scan consulted an out-of-depth directory", issues, err)
	}
	scope.Recursive = true
	_, _, issues, _, err = access.EntriesWithExclusions(context.Background(), scope)
	if err == nil && len(issues) == 0 {
		t.Fatal("fixture did not prevent included directory inspection")
	}
}

func TestWindowsHiddenSymlinkDoesNotBlockScan(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "hidden-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("symbolic links unavailable: " + err.Error())
	}
	name, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := syscall.GetFileAttributes(name)
	if err != nil || syscall.SetFileAttributes(name, attributes|syscall.FILE_ATTRIBUTE_HIDDEN) != nil {
		t.Fatal("cannot set hidden symlink attribute", err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, SkipHidden: true}
	entries, _, issues, excluded, err := access.EntriesWithExclusions(context.Background(), scope)
	if err != nil || len(entries) != 0 || len(issues) != 0 || len(excluded) != 1 {
		t.Fatal("hidden Windows symlink blocked scan or followed target", issues, excluded, err)
	}
	if err := syscall.SetFileAttributes(name, attributes); err != nil {
		t.Fatal(err)
	}
	_, _, issues, _, err = access.EntriesWithExclusions(context.Background(), scope)
	if err != nil || len(issues) != 1 {
		t.Fatal("visible symlink did not retain incomplete-scan behavior", issues, err)
	}
}

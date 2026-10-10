package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func TestWatchDirectoriesReuseDepthExclusionsAndRootAuthorization(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"allowed/deep", "private/secret", "other"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	linkErr := os.Symlink(outside, filepath.Join(root, "link"))
	if linkErr != nil {
		t.Log("Symbolic link setup unavailable; remaining scope checks still run")
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, MaxDepth: 1, ExcludePatterns: []string{"private"}}
	dirs, err := access.WatchDirectories(context.Background(), scope, 256)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, dir := range dirs {
		names = append(names, dir.RelativePath)
		if info, err := dir.File.Stat(); err != nil || !info.IsDir() {
			t.Fatal("not an open directory")
		}
		dir.File.Close()
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{".", "allowed", "other"}) {
		t.Fatalf("scope leaked: %v", names)
	}
	if dirs, err = access.WatchDirectories(context.Background(), scope, 2); err == nil || dirs != nil {
		t.Fatal("directory budget not atomic")
	}
	for _, mutate := range []func(*pluginproto.ScanScope){func(s *pluginproto.ScanScope) { s.RootIdentity = "wrong" }, func(s *pluginproto.ScanScope) { s.RootID = "outside" }} {
		bad := scope
		mutate(&bad)
		if dirs, err = access.WatchDirectories(context.Background(), bad, 256); err == nil || dirs != nil {
			t.Fatal("unauthorized watch root accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if dirs, err = access.WatchDirectories(ctx, scope, 256); err == nil || dirs != nil {
		t.Fatal("cancelled enumeration accepted")
	}
	scope.Recursive, scope.MaxDepth = false, 0
	dirs, err = access.WatchDirectories(context.Background(), scope, 1)
	if err != nil || len(dirs) != 1 {
		t.Fatal("flat root watch failed", err)
	}
	for _, dir := range dirs {
		dir.File.Close()
	}
}

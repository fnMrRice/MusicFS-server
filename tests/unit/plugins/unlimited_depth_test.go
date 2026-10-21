package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestUnlimitedDepthEnumeratesAndWatchesBeyond64(t *testing.T) {
	root := t.TempDir()
	deepest := filepath.Join(root, filepath.FromSlash(strings.Repeat("d/", 70)))
	if err := os.MkdirAll(deepest, 0700); err != nil {
		t.Fatal(err)
	}
	for _, depth := range []int{0, 64, 65, 70} {
		file := filepath.Join(root, filepath.FromSlash(strings.Repeat("d/", depth)+"song.wav"))
		if err := os.WriteFile(file, []byte("source bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		recursive   bool
		depth       int
		files, dirs int
	}{
		{"unlimited", true, 0, 4, 71},
		{"bounded", true, 64, 2, 65},
		{"flat", false, 0, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: tc.recursive, MaxDepth: tc.depth}
			entries, _, issues, err := access.Entries(context.Background(), scope)
			if err != nil || len(issues) != 0 || len(entries) != tc.files {
				t.Fatalf("scan did not honor depth: files=%d issues=%v err=%v", len(entries), issues, err)
			}
			dirs, err := access.WatchDirectories(context.Background(), scope, 256)
			for _, dir := range dirs {
				dir.File.Close()
			}
			if err != nil || len(dirs) != tc.dirs {
				t.Fatalf("watch did not honor depth: dirs=%d err=%v", len(dirs), err)
			}
		})
	}
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true}
	if dirs, err := access.WatchDirectories(context.Background(), scope, 64); err == nil || dirs != nil {
		t.Fatal("unlimited recursion bypassed the watch handle budget")
	}
	scope.ExcludePatterns = []string{"d"}
	entries, _, issues, err := access.Entries(context.Background(), scope)
	if err != nil || len(issues) != 0 || len(entries) != 1 {
		t.Fatal("unlimited recursion bypassed exclusions", err)
	}
}

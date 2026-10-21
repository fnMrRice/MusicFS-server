package plugins_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestRootOptionsDriveIndependentEnumerationWatchAndRequestOverrides(t *testing.T) {
	ctx := context.Background()
	executable := buildWorker(t)
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	roots := []sourceaccess.RootConfig{{ID: "flat", Path: t.TempDir()}, {ID: "deep", Path: t.TempDir()}}
	for _, root := range roots {
		if err := os.Mkdir(filepath.Join(root.Path, "sub"), 0700); err != nil {
			t.Fatal(err)
		}
		wave(t, filepath.Join(root.Path, "song.wav"))
		wave(t, filepath.Join(root.Path, "sub", "song.wav"))
	}
	worker, err := pluginhost.Start(ctx, pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sha256.Sum256(binary)), InstanceID: "4bac928d-f959-5df3-9077-25730ec700e1", UserID: "owner", Roots: roots, StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	declaration := pluginconfig.LocalFiles()
	flat := declaration.ImportDefaults
	flat.Recursive = false
	runtime := library.Runtime{ID: "local", Owner: "owner", Worker: worker, Configuration: declaration, ConfigRevision: "root-defaults-v1", ImportDefaults: flat, RootImportDefaults: map[string]pluginproto.ImportOptions{"deep": declaration.ImportDefaults}}
	service := library.New(nil, jobs.New(nil), []library.Runtime{runtime}, nil)
	input := library.ScanInput{Mode: "full", SchemaRevision: declaration.SchemaRevision, RootIDs: []string{"flat", "deep"}}
	plan, err := service.BuildScanPlan(ctx, "owner", "local", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range plan.Roots {
		scope, err := plan.Scope(root.RootID)
		if err != nil {
			t.Fatal(err)
		}
		entries, _, issues, err := worker.Access.Entries(ctx, scope)
		want := 1
		if root.RootID == "deep" {
			want = 2
		}
		if err != nil || len(issues) != 0 || len(entries) != want {
			t.Fatal("root enumeration did not apply independent recursion", root.RootID, len(entries), issues, err)
		}
		dirs, err := worker.Access.WatchDirectories(ctx, scope, 256)
		for _, dir := range dirs {
			dir.File.Close()
		}
		if err != nil || len(dirs) != want {
			t.Fatal("watch coverage differs from scan coverage", root.RootID, len(dirs), err)
		}
	}
	// A request override merges onto each root, not the unused flat default.
	depth := 1
	input.RootIDs = []string{"deep"}
	input.Options.MaxDepth = &depth
	plan, err = service.BuildScanPlan(ctx, "owner", "local", input)
	if err != nil || plan.RootOptions["deep"].MaxDepth != 1 {
		t.Fatal("instance default incorrectly rejected a valid selected root", err)
	}
	// Default fallback remains stable when requests reorder overridden roots.
	runtime.RootImportDefaults["flat"] = declaration.ImportDefaults
	if err := service.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	input.RootIDs = []string{"flat", "deep"}
	first, err := service.BuildScanPlan(ctx, "owner", "local", input)
	if err != nil {
		t.Fatal(err)
	}
	input.RootIDs = []string{"deep", "flat"}
	second, err := service.BuildScanPlan(ctx, "owner", "local", input)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := first.Digest()
	two, _ := second.Digest()
	if one != two {
		t.Fatal("root ordering changed equivalent scan digest")
	}
}

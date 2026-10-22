package sourceaccess_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestPackageNodeBudgetCountsExcludedEntries(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < sourceaccess.MaxPackageNodes; i++ {
		packageWrite(t, root, fmt.Sprintf(".hidden%04d", i), nil)
	}
	_, err := packageAccess(t, root).PackageSnapshot(context.Background(), "music", ".", pluginproto.ImportOptions{SkipHidden: true})
	packageErrorCode(t, err, "RATE_LIMITED")
}

func TestPackageAggregatePathBudget(t *testing.T) {
	root := t.TempDir()
	relative := strings.Repeat(strings.Repeat("a", 65)+"/", 30)
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		packageWrite(t, root, fmt.Sprintf("%sf%04d.txt", relative, i), nil)
	}
	_, err := packageAccess(t, root).PackageSnapshot(context.Background(), "music", ".", pluginproto.ImportOptions{})
	packageErrorCode(t, err, "RATE_LIMITED")
}

func TestPackageRootReplacementIsRejected(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	a := packageAccess(t, root)
	if err := os.Rename(root, filepath.Join(parent, "old-root")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PackageSnapshot(context.Background(), "music", ".", pluginproto.ImportOptions{}); err == nil {
		t.Fatal("accepted a replaced root identity")
	}
}

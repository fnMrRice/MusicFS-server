package workpackages_test

import (
	"fmt"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

func TestPackageLimitDoesNotBlockRefresh(t *testing.T) {
	store, work, _ := fixture(t)
	empty, err := store.WorkPackages(t.Context(), "alice", work.ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty package list must be an array", err)
	}
	var first workpackages.Package
	for i := 0; i < workpackages.MaxPackages; i++ {
		input := publication(work.ID)
		name := fmt.Sprintf("package-%d", i)
		input.Package.RelativePath, input.Package.Name = name, name
		input.Package.Nodes = []workpackages.Node{{Path: ".", Name: name, SourcePath: name, Kind: "directory"}}
		input.Observations = nil
		created := publish(t, store, "alice", input)
		if i == 0 {
			first = created
		}
	}
	_, err = store.PublishWorkPackage(t.Context(), "alice", publication(work.ID), "overflow-preview", "overflow-key")
	requireCode(t, err, "RATE_LIMITED")
	refresh := workpackages.Publication{Package: first, ExpectedWorkRevision: 1, ExpectedPackageRevision: first.Revision}
	updated := publish(t, store, "alice", refresh)
	if updated.ID != first.ID || updated.Revision != 2 {
		t.Fatal("refresh failed at package limit")
	}
	all, err := store.WorkPackages(t.Context(), "alice", work.ID)
	if err != nil || len(all) != workpackages.MaxPackages {
		t.Fatal("package limit was exceeded", err)
	}
}

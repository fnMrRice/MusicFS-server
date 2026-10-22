package workpackages_test

import (
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPackageImportRollbackIncludesTracksAssetsTreeAndReceipt(t *testing.T) {
	store, work, _ := fixture(t)
	input := publication(work.ID)
	if _, err := store.DB.ExecContext(t.Context(), `CREATE TRIGGER reject_package BEFORE INSERT ON work_packages BEGIN SELECT RAISE(ABORT,'test publication failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err := store.PublishWorkPackage(t.Context(), "alice", input, "rollback-preview", "rollback-import")
	if err == nil {
		t.Fatal("forced publication failure succeeded")
	}
	for _, table := range []string{"work_packages", "tracks", "source_assets"} {
		var count int
		if err = store.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed import left records", table, count, err)
		}
	}
	_, found, err := store.ReplayWorkPackage(t.Context(), "alice", work.ID, "rollback-preview", "rollback-import")
	if err != nil || found {
		t.Fatal("failed import wrote a receipt", err)
	}
	if _, err = store.DB.ExecContext(t.Context(), "DROP TRIGGER reject_package"); err != nil {
		t.Fatal(err)
	}
	first, err := store.PublishWorkPackage(t.Context(), "alice", input, "rollback-preview", "rollback-import")
	if err != nil {
		t.Fatal(err)
	}
	input.Package, input.ExpectedPackageRevision = first, first.Revision
	input.Package.Nodes = append(input.Package.Nodes[:0:0], first.Nodes...)
	input.Package.Nodes[2].Version = "changed-version"
	refreshObservations(&input)
	if _, err = store.DB.ExecContext(t.Context(), `CREATE TRIGGER reject_package_update BEFORE UPDATE ON work_packages BEGIN SELECT RAISE(ABORT,'test update failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = store.PublishWorkPackage(t.Context(), "alice", input, "update-preview", "update-import")
	if err == nil {
		t.Fatal("forced tree replacement failure succeeded")
	}
	actual, err := store.WorkPackage(t.Context(), "alice", work.ID, first.ID)
	if err != nil || !reflect.DeepEqual(actual, first) {
		t.Fatal("failed update changed the tree", err)
	}
	track, err := store.FindTrack(t.Context(), "alice", first.Nodes[2].SourceTrackID)
	if err != nil || track.AssetID != first.Nodes[2].AssetID || !track.Asset.Current {
		t.Fatal("failed update replaced a source asset", err)
	}
}

func TestPackageImportRejectsWorkEditsAndActiveProviderScan(t *testing.T) {
	store, work, _ := fixture(t)
	input := publication(work.ID)
	if _, err := store.UpdateWork(t.Context(), "alice", work.ID, works.UpdateRequest{ExpectedRevision: 1, Title: "Edited work"}, "work-edit-key"); err != nil {
		t.Fatal(err)
	}
	_, err := store.PublishWorkPackage(t.Context(), "alice", input, "work-preview", "work-import")
	requireCode(t, err, "CONFLICT")
	input.ExpectedWorkRevision = 2
	if _, _, err = store.Enqueue(t.Context(), "alice", input.Package.ProviderID, testkit.ScanPlan("music"), "scan-before-import"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PublishWorkPackage(t.Context(), "alice", input, "scan-preview", "scan-import")
	requireCode(t, err, "CONFLICT")
	if err = store.Finish(t.Context(), job, 0, nil); err != nil {
		t.Fatal(err)
	}
	publish(t, store, "alice", input)
}

func TestConcurrentPackageImportsHaveOneWinner(t *testing.T) {
	store, work, _ := fixture(t)
	input := publication(work.ID)
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.PublishWorkPackage(t.Context(), "alice", input, domain.NewID(), domain.NewID())
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else {
			requireCode(t, err, "CONFLICT")
		}
	}
	if winners != 1 {
		t.Fatal("concurrent imports did not preserve location uniqueness", winners)
	}
	items, err := store.WorkPackages(t.Context(), "alice", work.ID)
	if err != nil || len(items) != 1 || items[0].Revision != 1 {
		t.Fatal("concurrent import left an invalid package", err)
	}
}

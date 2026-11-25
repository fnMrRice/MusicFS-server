package workpackages_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestPackageKindFilteringUsesOwnerScopedIntersectionWithoutDuplicateWorks(t *testing.T) {
	store, voice, _ := fixture(t)
	disc, err := store.CreateWork(t.Context(), "alice", works.CreateRequest{ContentType: "cd_album", Title: "Rip album", Tags: []string{"archive"}, Credits: []works.Credit{{Role: "artist", Name: "Band"}}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	digital := createWork(t, store, "alice")
	createWork(t, store, "alice")
	private := createWork(t, store, "bob")
	add := func(owner, id, location, kind string) {
		t.Helper()
		input := publication(id)
		input.Package.Kind, input.Package.RelativePath = kind, location
		input.Package.Nodes = input.Package.Nodes[:1]
		input.Package.Nodes[0].SourcePath = location
		input.Observations = nil
		publish(t, store, owner, input)
	}
	add("alice", disc.ID, "album-cd1", "cd_dump")
	add("alice", disc.ID, "album-cd2", "cd_dump")
	add("alice", disc.ID, "album-digital", "directory")
	add("alice", voice.ID, "voice-cd", "cd_dump")
	add("alice", digital.ID, "digital", "directory")
	add("bob", private.ID, "album-cd1", "cd_dump")
	for _, test := range []struct {
		name  string
		query works.Query
		want  []string
	}{
		{"all-content-types", works.Query{PackageKind: "cd_dump"}, []string{disc.ID, voice.ID}},
		{"cd-album-intersection", works.Query{PackageKind: "cd_dump", ContentType: "cd_album"}, []string{disc.ID}},
		{"asmr-intersection", works.Query{PackageKind: "cd_dump", ContentType: "asmr"}, []string{voice.ID}},
		{"metadata-intersection", works.Query{PackageKind: "cd_dump", Tag: "archive", Q: "rip", Credit: "Band"}, []string{disc.ID}},
		{"disjoint", works.Query{PackageKind: "cd_dump", ContentType: "asmr", Tag: "archive"}, []string{}},
		{"directory", works.Query{PackageKind: "directory"}, []string{disc.ID, digital.ID}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.Limit = 100
			items, err := store.Works(t.Context(), "alice", test.query)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, item := range items {
				ids = append(ids, item.ID)
			}
			sort.Strings(test.want)
			if !reflect.DeepEqual(ids, test.want) {
				t.Fatalf("work IDs = %v, want %v", ids, test.want)
			}
		})
	}
	want := []string{disc.ID, voice.ID}
	sort.Strings(want)
	after := ""
	for _, id := range want {
		page, err := store.Works(t.Context(), "alice", works.Query{PackageKind: "cd_dump", Limit: 1, After: after})
		if err != nil || len(page) != 1 || page[0].ID != id {
			t.Fatal("filtered pagination skipped or duplicated a work", err)
		}
		after = id
	}
	page, err := store.Works(t.Context(), "alice", works.Query{PackageKind: "cd_dump", Limit: 1, After: after})
	if err != nil || len(page) != 0 {
		t.Fatal("filtered pagination did not finish", err)
	}
	_, err = store.Works(t.Context(), "alice", works.Query{PackageKind: "physical_disc", Limit: 10})
	requireCode(t, err, "INVALID_ARGUMENT")
}

func TestPackageKindRefreshReclassifiesOneCopyAndPreservesReplay(t *testing.T) {
	store, work, _ := fixture(t)
	input := publication(work.ID)
	input.Package.Kind = "cd_dump"
	first, err := store.PublishWorkPackage(t.Context(), "alice", input, "cd-preview", "cd-import-key")
	if err != nil {
		t.Fatal(err)
	}
	input.Package, input.ExpectedPackageRevision = first, first.Revision
	input.Package.Kind = "directory"
	next := publish(t, store, "alice", input)
	if next.ID != first.ID || next.Kind != "directory" || next.Revision != 2 || !reflect.DeepEqual(next.Nodes, first.Nodes) {
		t.Fatal("reclassification changed the copy or source identities")
	}
	listed, err := store.Works(t.Context(), "alice", works.Query{PackageKind: "cd_dump", Limit: 10})
	if err != nil || len(listed) != 0 {
		t.Fatal("reimport left a stale kind index", err)
	}
	read, err := store.WorkPackage(t.Context(), "alice", work.ID, next.ID)
	if err != nil || read.Kind != "directory" {
		t.Fatal("reimport kind was not persisted", err)
	}
	replay, found, err := store.ReplayWorkPackage(t.Context(), "alice", work.ID, "cd-preview", "cd-import-key")
	if err != nil || !found || replay.Kind != "cd_dump" || replay.Revision != 1 {
		t.Fatal("reclassification changed the original idempotent response", err)
	}
	actual, err := store.Work(t.Context(), "alice", work.ID)
	if err != nil || actual.ContentType != "asmr" || actual.Revision != 1 {
		t.Fatal("copy kind changed the work content type", err)
	}
}

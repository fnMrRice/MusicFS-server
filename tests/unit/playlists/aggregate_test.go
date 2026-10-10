package playlists_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func stringPointer(v string) *string { return &v }

func TestPlaylistAggregateReferencesAndHiddenScopesAreStrict(t *testing.T) {
	for _, ref := range []playlists.SourceRef{{}, {SourceTrackID: stringPointer("source"), RemoteSourceID: stringPointer("remote")}, {SourceTrackID: stringPointer("")}} {
		if ref.Validate() == nil {
			t.Fatal("invalid reference accepted")
		}
	}
	local := playlists.SourceRef{SourceTrackID: stringPointer("source")}
	remote := playlists.SourceRef{RemoteSourceID: stringPointer("source")}
	if local.Key() == remote.Key() {
		t.Fatal("source namespaces collide")
	}
	for _, request := range []playlists.HideRequest{
		{Scope: "allSources", Source: local, BindingPath: []string{"binding"}},
		{Scope: "binding", Source: local},
		{Scope: "entry", Source: local, BindingPath: []string{"binding"}},
		{Scope: "unknown", Source: local},
	} {
		if request.Validate() == nil {
			t.Fatal("invalid hidden scope accepted")
		}
	}
	request := playlists.HideRequest{Scope: "entry", Source: local, BindingPath: []string{"binding"}, EntryID: stringPointer("entry")}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlaylistAggregatePageRetainsAllVisibleProvenance(t *testing.T) {
	snapshot := playlists.AggregateSnapshot{Revision: 4, Entries: []playlists.AggregateEntry{
		{ID: "a", Hidden: true},
		{ID: "b", Provenance: []playlists.Provenance{{Hidden: true}, {Hidden: false}}},
		{ID: "c"},
	}}
	page, err := playlists.AggregateSnapshotPage(snapshot, 0, 1)
	if err != nil || page.Total != 2 || page.Revision != 4 || len(page.Items) != 1 || len(page.Items[0].Provenance) != 2 || page.NextOffset == nil || *page.NextOffset != 1 {
		t.Fatal("page lost immutable provenance", err)
	}
	if _, err = playlists.AggregateSnapshotPage(snapshot, 3, 1); err == nil {
		t.Fatal("invalid offset accepted")
	}
}

package storage_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func TestAlbumMemberOrderSurvivesReadsReordersAndAudit(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	ids := []string{tracks[0].ID, tracks[1].ID, tracks[2].ID}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	a := albumCreate(t, s, "admin", "local", "Ordered", ids...)
	got, err := s.SourceAlbum(ctx, "admin", a.ID)
	if err != nil || !reflect.DeepEqual(got.SourceTrackIDs, ids) {
		t.Fatal("source album order was sorted", err)
	}
	b := albumCreate(t, s, "admin", "local", "Second")
	albumsOrder := []string{a.ID, b.ID}
	sort.Sort(sort.Reverse(sort.StringSlice(albumsOrder)))
	release, err := s.CreateRelease(ctx, "admin", albums.CreateReleaseRequest{Name: "Ordered edition", SourceAlbumIDs: albumsOrder}, "ordered-release-create")
	if err != nil {
		t.Fatal(err)
	}
	reread, err := s.Release(ctx, "admin", release.ID)
	if err != nil || !reflect.DeepEqual(reread.SourceAlbumIDs, albumsOrder) {
		t.Fatal("release order was sorted", err)
	}
	reordered := []string{ids[2], ids[0], ids[1]}
	_, err = s.UpdateSourceAlbum(ctx, "admin", a.ID, albums.UpdateSourceAlbumRequest{ExpectedRevision: 1, Name: a.Name, SourceTrackIDs: reordered}, "ordered-album-update")
	if err != nil {
		t.Fatal(err)
	}
	reread, err = s.Release(ctx, "admin", release.ID)
	if err != nil || reread.Revision != 2 || !reflect.DeepEqual(reread.SourceAlbumIDs, albumsOrder) {
		t.Fatal("member reorder lost dependent revision or edition order", err)
	}
	expansion, err := s.ExpandSourceAlbum(ctx, "admin", a.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, target := range expansion.Targets {
		if target.SourceTrackID != reordered[i] {
			t.Fatal("scrape expansion lost track order")
		}
	}
	changes, err := s.AlbumChanges(ctx, "admin", albums.Target{Kind: "sourceAlbum", ID: a.ID}, 0, 101)
	if err != nil || len(changes) != 2 || !reflect.DeepEqual(changes[1].PreviousMembers, ids) || !reflect.DeepEqual(changes[1].Members, reordered) {
		t.Fatal("reorder audit lost original positions", err)
	}
	reversed := []string{albumsOrder[1], albumsOrder[0]}
	request := albums.UpdateReleaseRequest{ExpectedRevision: 2, Name: release.Name, SourceAlbumIDs: reversed}
	updated, err := s.UpdateRelease(ctx, "admin", release.ID, request, "ordered-release-update")
	if err != nil || updated.Revision != 3 {
		t.Fatal(err)
	}
	request.SourceAlbumIDs = albumsOrder
	_, err = s.UpdateRelease(ctx, "admin", release.ID, request, "ordered-release-update")
	matchingError(t, err, "CONFLICT")
	reread, err = s.Release(ctx, "admin", release.ID)
	if err != nil || !reflect.DeepEqual(reread.SourceAlbumIDs, reversed) {
		t.Fatal("release reorder did not persist", err)
	}
	_, err = s.UpdateRelease(ctx, "admin", release.ID, request, domain.NewID())
	matchingError(t, err, "CONFLICT")
}

package storage_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func albumCreate(t *testing.T, s *sqlite.Store, owner, provider, name string, ids ...string) albums.SourceAlbum {
	t.Helper()
	a, err := s.CreateSourceAlbum(context.Background(), owner, albums.CreateSourceAlbumRequest{ProviderInstanceID: provider, Name: name, SourceTrackIDs: ids}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func albumOtherProvider(t *testing.T, s *sqlite.Store) library.Track {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.Enqueue(ctx, "admin", "other-provider", testkit.ScanPlan("music"), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{evidenceObservation("other", "v1")})); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, j, 1, nil); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListTracks(ctx, "admin", "other-provider", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	return p.Items[0]
}
func TestAlbumsExplicitIdentityOwnerProviderAndIdempotency(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	other := albumOtherProvider(t, s)
	r := albums.CreateSourceAlbumRequest{ProviderInstanceID: "local", Name: " 同名专辑 ", SourceTrackIDs: []string{tracks[1].ID, tracks[0].ID}}
	first, err := s.CreateSourceAlbum(ctx, "admin", r, "album-create-once")
	if err != nil {
		t.Fatal(err)
	}
	r.Name = "同名专辑"
	replay, err := s.CreateSourceAlbum(ctx, "admin", r, "album-create-once")
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("canonical replay changed identity", err)
	}
	r.SourceTrackIDs = []string{tracks[0].ID, tracks[1].ID}
	_, err = s.CreateSourceAlbum(ctx, "admin", r, "album-create-once")
	matchingError(t, err, "CONFLICT")
	second := albumCreate(t, s, "admin", "local", r.Name, tracks[0].ID)
	if first.ID == second.ID || first.Name != "同名专辑" {
		t.Fatal("names became identity keys")
	}
	r.Name = "Different"
	_, err = s.CreateSourceAlbum(ctx, "admin", r, "album-create-once")
	matchingError(t, err, "CONFLICT")
	for _, test := range []struct {
		ids  []string
		code string
	}{{[]string{tracks[0].ID, tracks[0].ID}, "INVALID_ARGUMENT"}, {[]string{private.ID}, "NOT_FOUND"}, {[]string{other.ID}, "INVALID_ARGUMENT"}} {
		_, err = s.CreateSourceAlbum(ctx, "admin", albums.CreateSourceAlbumRequest{ProviderInstanceID: "local", Name: "Denied", SourceTrackIDs: test.ids}, domain.NewID())
		matchingError(t, err, test.code)
	}
	if _, err = s.SourceAlbum(ctx, "bob", first.ID); err == nil {
		t.Fatal("foreign album visible")
	}
	list, err := s.SourceAlbums(ctx, "bob", "", "", 101)
	if err != nil || len(list) != 0 {
		t.Fatal("foreign list leaked", err)
	}
	list, err = s.SourceAlbums(ctx, "admin", "local", "", 1)
	if err != nil || len(list) != 1 {
		t.Fatal(err)
	}
	last, err := s.SourceAlbums(ctx, "admin", "local", list[0].ID, 1)
	if err != nil || len(last) != 1 || list[0].ID == last[0].ID {
		t.Fatal("invalid keyset page", err)
	}
	empty := albumCreate(t, s, "admin", "local", "Empty")
	if empty.SourceTrackIDs == nil || len(empty.SourceTrackIDs) != 0 {
		t.Fatal("empty members must be an array")
	}
}
func TestReleaseDependencyRevisionsAndAtomicMemberChanges(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	a := albumCreate(t, s, "admin", "local", "Edition A", tracks[0].ID)
	b := albumCreate(t, s, "admin", "local", "Edition B", tracks[1].ID)
	release, err := s.CreateRelease(ctx, "admin", albums.CreateReleaseRequest{Name: "Edition", SourceAlbumIDs: []string{a.ID, b.ID}}, "release-create-once")
	if err != nil {
		t.Fatal(err)
	}
	request := albums.UpdateSourceAlbumRequest{ExpectedRevision: 1, Name: a.Name, SourceTrackIDs: []string{tracks[1].ID}}
	next, err := s.UpdateSourceAlbum(ctx, "admin", a.ID, request, "album-members-once")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.Release(ctx, "admin", release.ID)
	if err != nil || changed.Revision != 2 {
		t.Fatal("nested member change did not revise release", err)
	}
	if _, err = s.UpdateRelease(ctx, "admin", release.ID, albums.UpdateReleaseRequest{ExpectedRevision: 1, Name: "stale", SourceAlbumIDs: release.SourceAlbumIDs}, domain.NewID()); err == nil {
		t.Fatal("stale release update won")
	}
	replay, err := s.UpdateSourceAlbum(ctx, "admin", a.ID, request, "album-members-once")
	if err != nil || !reflect.DeepEqual(next, replay) {
		t.Fatal("member replay changed result", err)
	}
	still, _ := s.Release(ctx, "admin", release.ID)
	if still.Revision != 2 {
		t.Fatal("replay incremented parent revision")
	}
	expanded, err := s.ExpandSourceAlbum(ctx, "admin", a.ID, 2)
	if err != nil || len(expanded.Targets) != 1 || expanded.Targets[0].SourceTrackID != tracks[1].ID {
		t.Fatal("incorrect member expansion", err)
	}
	_, err = s.ExpandSourceAlbum(ctx, "admin", a.ID, 1)
	matchingError(t, err, "CONFLICT")
	foreign := albumCreate(t, s, "bob", "private", "Private")
	_, err = s.UpdateRelease(ctx, "admin", release.ID, albums.UpdateReleaseRequest{ExpectedRevision: 2, Name: "no partial update", SourceAlbumIDs: []string{foreign.ID}}, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	unchanged, _ := s.Release(ctx, "admin", release.ID)
	if !reflect.DeepEqual(still, unchanged) {
		t.Fatal("failed release update changed members")
	}
	changes, err := s.AlbumChanges(ctx, "admin", albums.Target{Kind: "release", ID: release.ID}, 0, 101)
	if err != nil || len(changes) != 2 || changes[1].Operation != "memberAlbumUpdated" {
		t.Fatal("missing nested membership audit", err)
	}
	_, err = s.AlbumChanges(ctx, "bob", albums.Target{Kind: "release", ID: release.ID}, 0, 101)
	matchingError(t, err, "NOT_FOUND")
}
func TestAlbumConcurrentRevisionAllowsOneWriter(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	a := albumCreate(t, s, "admin", "local", "Original", tracks[0].ID)
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, err := s.UpdateSourceAlbum(ctx, "admin", a.ID, albums.UpdateSourceAlbumRequest{ExpectedRevision: 1, Name: name, SourceTrackIDs: []string{tracks[1].ID}}, domain.NewID())
			out <- err
		}(name)
	}
	wg.Wait()
	close(out)
	succeeded := 0
	for err := range out {
		if err == nil {
			succeeded++
		} else {
			matchingError(t, err, "CONFLICT")
		}
	}
	if succeeded != 1 {
		t.Fatal("lost revision serialization", succeeded)
	}
	changes, err := s.AlbumChanges(ctx, "admin", albums.Target{Kind: "sourceAlbum", ID: a.ID}, 0, 101)
	if err != nil || len(changes) != 2 {
		t.Fatal("failed writer left audit", err)
	}
}

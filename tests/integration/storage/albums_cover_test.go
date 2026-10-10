package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func albumArtwork(t *testing.T, s *sqlite.Store, owner string, track library.Track) mediastore.ContentObject {
	t.Helper()
	input := contentInput(track, "", "validated image descriptor")
	input.Metadata.Field = "artwork"
	input.Metadata.Format = "png"
	input.MIME = "image/png"
	return contentPublish(t, s, owner, input)
}
func albumPass(publish func() error) error { return publish() }
func TestAlbumCoverEditionScopeSurvivesMembershipAndAssetChanges(t *testing.T) {
	s, items, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	a := albumCreate(t, s, "admin", "local", "Same Name", tracks[0].ID)
	b := albumCreate(t, s, "admin", "local", "Same Name", tracks[1].ID)
	first, second := albumArtwork(t, s, "admin", tracks[0]), albumArtwork(t, s, "admin", tracks[1])
	target := albums.Target{Kind: "sourceAlbum", ID: a.ID}
	initial, err := s.Cover(ctx, "admin", target)
	if err != nil || initial.Revision != 0 || initial.Binding != nil || initial.ManualLocked {
		t.Fatal("unexpected initial cover", err)
	}
	chosen, err := s.SelectCover(ctx, "admin", albums.SelectCoverRequest{Target: target, ObjectID: first.ID, ExpectedTargetRevision: 1}, "album-cover-first", albumPass)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SelectCover(ctx, "admin", albums.SelectCoverRequest{Target: target, ObjectID: second.ID, ExpectedRevision: 1, ExpectedTargetRevision: 1}, domain.NewID(), albumPass)
	matchingError(t, err, "INVALID_ARGUMENT")
	release, err := s.CreateRelease(ctx, "admin", albums.CreateReleaseRequest{Name: "Release", SourceAlbumIDs: []string{a.ID, b.ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	releaseTarget := albums.Target{Kind: "release", ID: release.ID}
	releaseCover, err := s.SelectCover(ctx, "admin", albums.SelectCoverRequest{Target: releaseTarget, ObjectID: second.ID, ExpectedTargetRevision: 1}, domain.NewID(), albumPass)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateSourceAlbum(ctx, "admin", a.ID, albums.UpdateSourceAlbumRequest{ExpectedRevision: 1, Name: a.Name, SourceTrackIDs: []string{}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	items[0].ObservedVersion = "v2"
	publishEvidenceTracks(t, s, items...)
	retained, err := s.Cover(ctx, "admin", target)
	if err != nil || !reflect.DeepEqual(retained, chosen) {
		t.Fatal("member/asset changes erased album artwork intent", err)
	}
	retainedRelease, err := s.Cover(ctx, "admin", releaseTarget)
	if err != nil || !reflect.DeepEqual(retainedRelease, releaseCover) {
		t.Fatal("recording/member changes overwrote release artwork", err)
	}
	_, err = s.SelectCover(ctx, "admin", albums.SelectCoverRequest{Target: releaseTarget, ObjectID: second.ID, ExpectedRevision: 1, ExpectedTargetRevision: 1}, domain.NewID(), albumPass)
	matchingError(t, err, "CONFLICT")
	if _, err = s.UpdateSourceAlbum(ctx, "admin", a.ID, albums.UpdateSourceAlbumRequest{ExpectedRevision: 2, Name: a.Name, SourceTrackIDs: []string{tracks[0].ID}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	_, err = s.SelectCover(ctx, "admin", albums.SelectCoverRequest{Target: target, ObjectID: first.ID, ExpectedRevision: 1, ExpectedTargetRevision: 3}, domain.NewID(), albumPass)
	matchingError(t, err, "RESOURCE_CHANGED")
	if err = s.InvalidateContent(ctx, "admin", first.ID); err != nil {
		t.Fatal(err)
	}
	retained, err = s.Cover(ctx, "admin", target)
	if err != nil || retained.Binding.State != "unavailable" || !retained.ManualLocked || retained.Binding.AssetID != tracks[0].AssetID {
		t.Fatal("invalidated artwork lost history or lock", err)
	}
	_, err = s.Cover(ctx, "bob", target)
	matchingError(t, err, "NOT_FOUND")
}
func TestAlbumCoverGuardManualLockAndReplay(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	a := albumCreate(t, s, "admin", "local", "Album", tracks[0].ID)
	object := albumArtwork(t, s, "admin", tracks[0])
	target := albums.Target{Kind: "sourceAlbum", ID: a.ID}
	r := albums.SelectCoverRequest{Target: target, ObjectID: object.ID, ExpectedTargetRevision: 1}
	guardCalls := 0
	fail := func(func() error) error { guardCalls++; return domain.Fail("RESOURCE_CHANGED", "Missing file") }
	_, err := s.SelectCover(ctx, "admin", r, "album-guard-choice", fail)
	matchingError(t, err, "RESOURCE_CHANGED")
	before, err := s.Cover(ctx, "admin", target)
	if err != nil || before.Binding != nil || before.Revision != 0 {
		t.Fatal("failed guard replaced selection", err)
	}
	_, err = s.SelectCover(ctx, "admin", r, "album-guard-noop", func(func() error) error { return nil })
	matchingError(t, err, "CONFLICT")
	committedInside := false
	selected, err := s.SelectCover(ctx, "admin", r, "album-guard-choice", func(publish func() error) error {
		guardCalls++
		if err := publish(); err != nil {
			return err
		}
		got, err := s.Cover(ctx, "admin", target)
		committedInside = err == nil && got.Revision == 1
		return err
	})
	if err != nil || !committedInside || guardCalls != 2 {
		t.Fatal("commit escaped file guard", err)
	}
	replay, err := s.SelectCover(ctx, "admin", r, "album-guard-choice", fail)
	if err != nil || !reflect.DeepEqual(selected, replay) || guardCalls != 2 {
		t.Fatal("replay reopened file", err)
	}
	r.ExpectedRevision = 1
	r.Automatic = true
	_, err = s.SelectCover(ctx, "admin", r, domain.NewID(), fail)
	matchingError(t, err, "CONFLICT")
	unlocked, err := s.UnlockCover(ctx, "admin", albums.UnlockCoverRequest{Target: target, ExpectedRevision: 1}, "album-unlock-once")
	if err != nil || unlocked.ManualLocked || unlocked.Revision != 2 {
		t.Fatal(err)
	}
	r.ExpectedRevision = 2
	_, err = s.SelectCover(ctx, "admin", r, domain.NewID(), fail)
	matchingError(t, err, "CONFLICT")
	r.Automatic = false
	r.ExpectedTargetRevision = 2
	_, err = s.SelectCover(ctx, "admin", r, domain.NewID(), fail)
	matchingError(t, err, "CONFLICT")
	r.ExpectedTargetRevision = 1
	lyrics := contentPublish(t, s, "admin", contentInput(tracks[0], "en", "lyrics"))
	r.ObjectID = lyrics.ID
	_, err = s.SelectCover(ctx, "admin", r, domain.NewID(), fail)
	matchingError(t, err, "INVALID_ARGUMENT")
	if guardCalls != 2 {
		t.Fatal("invalid or locked operation performed file IO")
	}
	changes, err := s.AlbumChanges(ctx, "admin", target, 0, 101)
	if err != nil || len(changes) != 3 || changes[1].Operation != "selectCover" || changes[2].Operation != "unlockCover" {
		t.Fatal("cover audit incomplete", err)
	}
}

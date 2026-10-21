package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestContentSelectionManualLocksAndLanguageSlots(t *testing.T) {
	s, items, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	zh := contentPublish(t, s, "admin", contentInput(tracks[0], "zh-Hans", "逐字歌词正文"))
	en := contentPublish(t, s, "admin", contentInput(tracks[0], "en", "English lyrics"))
	selection, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || selection.Revision != 0 || len(selection.Bindings) != 0 {
		t.Fatal("upload selected content implicitly", err)
	}
	selection = contentSelect(t, s, zh, 0, false)
	if selection.Revision != 1 || selection.Bindings[0].Language != "zh-hans" || !selection.Bindings[0].ManualLocked || !selection.Bindings[0].Current {
		t.Fatalf("manual selection missing: %+v", selection)
	}
	contentSelect(t, s, en, 1, true)
	later := contentPublish(t, s, "admin", contentInput(tracks[0], "ZH-HANS", "Late automatic lyrics"))
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: later.Metadata.Target, ExpectedRevision: 2, ObjectID: later.ID, Automatic: true}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	selection, err = s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: tracks[0].ID, ExpectedRevision: 2, Field: "lyrics", Language: "ZH-Hans"}, "unlock-zh-001")
	if err != nil || selection.Revision != 3 {
		t.Fatal(err)
	}
	// Removing a lock alone does not authorize replacing existing good content.
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: later.Metadata.Target, ExpectedRevision: 3, ObjectID: later.ID, Automatic: true}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	if err = s.InvalidateContent(ctx, "admin", zh.ID); err != nil {
		t.Fatal(err)
	}
	selection, err = s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || selection.Revision != 4 {
		t.Fatal("lost content failed to advance revision", err)
	}
	selection = contentSelect(t, s, later, 4, true)
	if len(selection.Bindings) != 2 || selection.Revision != 5 {
		t.Fatal("automatic repair removed another language")
	}
	// A later manual selection locks the slot for future source versions too.
	selection = contentSelect(t, s, later, 5, false)
	items[0].ObservedVersion = "v2"
	tracks = publishEvidenceTracks(t, s, items...)
	selection, err = s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || selection.Revision != 6 {
		t.Fatal(err)
	}
	for _, binding := range selection.Bindings {
		if binding.Current || binding.State != "stale" {
			t.Fatal("old source version remained current")
		}
	}
	newVersion := contentPublish(t, s, "admin", contentInput(tracks[0], "zh-hans", "Current version lyrics"))
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: newVersion.Metadata.Target, ExpectedRevision: 6, ObjectID: newVersion.ID, Automatic: true}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	if _, err = s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: tracks[0].ID, ExpectedRevision: 6, Field: "lyrics", Language: "zh-hans"}, "unlock-v2-001"); err != nil {
		t.Fatal(err)
	}
	selection = contentSelect(t, s, newVersion, 7, true)
	var oldCount int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM media_content_bindings WHERE current=0").Scan(&oldCount); err != nil || oldCount != 3 {
		t.Fatal("binding history was deleted", oldCount, err)
	}
	if selection.Revision != 8 {
		t.Fatal("selection revision did not include lock changes")
	}
}

func TestContentSelectionReplayScopeAndMissingObjects(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	object := contentPublish(t, s, "admin", contentInput(tracks[0], "", "first"))
	other := contentPublish(t, s, "admin", contentInput(tracks[1], "", "second"))
	privateObject := contentPublish(t, s, "bob", contentInput(private, "", "private body"))
	request := mediastore.SelectRequest{Target: object.Metadata.Target, ObjectID: object.ID}
	first, err := s.SelectContent(ctx, "admin", request, "select-replay-01")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.SelectContent(ctx, "admin", request, "select-replay-01")
	if err != nil || !reflect.DeepEqual(first, replayed) {
		t.Fatal("selection retry changed IDs/times", err)
	}
	request.Automatic = true
	_, err = s.SelectContent(ctx, "admin", request, "select-replay-01")
	matchingError(t, err, "CONFLICT")
	request.Automatic = false
	if err = s.InvalidateContent(ctx, "admin", object.ID); err != nil {
		t.Fatal(err)
	}
	invalid, err := s.ContentObject(ctx, "admin", object.ID)
	if err != nil || invalid.State != "unavailable" {
		t.Fatal("invalid object stayed available", err)
	}
	if err = s.InvalidateContent(ctx, "admin", object.ID); err != nil {
		t.Fatal(err)
	}
	selection, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || selection.Revision != 2 || !selection.Bindings[0].ManualLocked || selection.Bindings[0].State != "unavailable" {
		t.Fatal("invalidation lost lock or repeated revision", err)
	}
	replayed, err = s.SelectContent(ctx, "admin", request, "select-replay-01")
	if err != nil || !reflect.DeepEqual(first, replayed) {
		t.Fatal("prior idempotency result lost", err)
	}
	selection, _ = s.ContentSelection(ctx, "admin", tracks[0].ID)
	if selection.Revision != 2 || selection.Bindings[0].State != "unavailable" {
		t.Fatal("replay revived missing content")
	}
	_, err = s.ContentObject(ctx, "admin", privateObject.ID)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.ContentSelection(ctx, "admin", private.ID)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: object.Metadata.Target, ExpectedRevision: 2, ObjectID: privateObject.ID}, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: object.Metadata.Target, ExpectedRevision: 2, ObjectID: other.ID}, domain.NewID())
	matchingError(t, err, "RESOURCE_CHANGED")
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: object.Metadata.Target, ExpectedRevision: 2, ObjectID: object.ID}, domain.NewID())
	matchingError(t, err, "RESOURCE_CHANGED")
	_, err = s.UnlockContent(ctx, "bob", mediastore.UnlockRequest{SourceTrackID: tracks[0].ID, ExpectedRevision: 2, Field: "lyrics"}, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	if err = s.InvalidateContent(ctx, "bob", object.ID); err == nil {
		t.Fatal("cross-owner invalidation accepted")
	}
	unrelated, _ := s.ContentSelection(ctx, "admin", tracks[1].ID)
	if unrelated.Revision != 0 {
		t.Fatal("selection revisions leaked across sources")
	}
}

func TestContentUnspecifiedLyricsLanguageIsOneLockedSlot(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	upload := contentPublish(t, s, "admin", contentInput(tracks[0], "", "manually selected unspecified lyrics"))
	selection := contentSelect(t, s, upload, 0, false)
	if upload.Metadata.Language != "und" || selection.Bindings[0].Language != "und" {
		t.Fatal("unspecified lyrics language was not canonicalized")
	}
	scraped := contentPublish(t, s, "admin", contentInput(tracks[0], "UND", "automatic unspecified lyrics"))
	_, err := s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: scraped.Metadata.Target, ExpectedRevision: 1, ObjectID: scraped.ID, Automatic: true}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	selection, err = s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: tracks[0].ID, ExpectedRevision: 1, Field: "lyrics", Language: ""}, "unlock-empty-01")
	if err != nil || selection.Revision != 2 || len(selection.Bindings) != 1 || selection.Bindings[0].Language != "und" || selection.Bindings[0].ManualLocked {
		t.Fatal("empty language unlock missed canonical slot", err)
	}
	input := contentInput(tracks[0], "und", "same normalized request")
	p, _, err := s.PrepareContent(ctx, "admin", input, "upload-und-001")
	if err != nil {
		t.Fatal(err)
	}
	input.Metadata.Language = ""
	replayed, exists, err := s.PrepareContent(ctx, "admin", input, "upload-und-001")
	if err != nil || !exists || replayed.ID != p.ID {
		t.Fatal("empty and und upload had different operation identity", err)
	}
}

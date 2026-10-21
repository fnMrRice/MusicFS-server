package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestContentSelectionGuardRetainsOldBindingAndReplaysBeforeIO(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	old := contentPublish(t, s, "admin", contentInput(tracks[0], "", "old content"))
	one := contentSelect(t, s, old, 0, false)
	replacement := contentPublish(t, s, "admin", contentInput(tracks[0], "", "replacement content"))
	request := mediastore.SelectRequest{Target: replacement.Metadata.Target, ObjectID: replacement.ID, ExpectedRevision: 1}
	guardCalls := 0
	fail := func(publish func() error) error {
		guardCalls++
		return domain.Fail("RESOURCE_CHANGED", "Missing immutable content")
	}
	_, err := s.SelectContentGuarded(ctx, "admin", request, "guard-selection-01", fail)
	matchingError(t, err, "RESOURCE_CHANGED")
	current, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || !reflect.DeepEqual(current, one) || guardCalls != 1 {
		t.Fatal("failed file guard changed selection", err)
	}
	_, err = s.SelectContentGuarded(ctx, "admin", request, "guard-not-called", func(func() error) error { return nil })
	matchingError(t, err, "CONFLICT")
	committedInsideGuard := false
	pass := func(publish func() error) error {
		guardCalls++
		if err := publish(); err != nil {
			return err
		}
		// The transaction released its connection before the guard releases its
		// file; this read proves that the reference commit is inside the guard.
		got, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
		committedInsideGuard = err == nil && got.Revision == 2 && got.Bindings[0].ObjectID == replacement.ID
		return err
	}
	second, err := s.SelectContentGuarded(ctx, "admin", request, "guard-selection-01", pass)
	if err != nil || !committedInsideGuard || guardCalls != 2 {
		t.Fatal("commit escaped the guard", err)
	}
	replay, err := s.SelectContentGuarded(ctx, "admin", request, "guard-selection-01", fail)
	if err != nil || !reflect.DeepEqual(second, replay) || guardCalls != 2 {
		t.Fatal("successful retry reran file IO", err)
	}
	request.ExpectedRevision = 0
	_, err = s.SelectContentGuarded(ctx, "admin", request, domain.NewID(), fail)
	matchingError(t, err, "CONFLICT")
	if guardCalls != 2 {
		t.Fatal("stale DB operation reached file IO")
	}
	_, err = s.SelectContentGuarded(ctx, "bob", request, domain.NewID(), fail)
	matchingError(t, err, "NOT_FOUND")
	if guardCalls != 2 {
		t.Fatal("unauthorized operation reached file IO")
	}
}

func TestContentInputValidationOfflineAndAbortedOperation(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	valid := contentInput(tracks[0], "en", "valid body")
	for _, alter := range []func(*mediastore.UploadInput){
		func(v *mediastore.UploadInput) { v.Size = 0 },
		func(v *mediastore.UploadInput) { v.Size = 2 << 20 },
		func(v *mediastore.UploadInput) { v.MIME = "image/png" },
		func(v *mediastore.UploadInput) { v.SHA256 = "not-a-hash" },
		func(v *mediastore.UploadInput) { v.Metadata.Language = "../../private" },
		func(v *mediastore.UploadInput) { v.Metadata.Origin.CandidateID = "../secret" },
	} {
		bad := valid
		alter(&bad)
		if _, _, err := s.PrepareContent(ctx, "admin", bad, domain.NewID()); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	_, _, err := s.PrepareContent(ctx, "bob", valid, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	p, _, err := s.PrepareContent(ctx, "admin", valid, "aborted-upload-01")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationStage(ctx, "admin", p.ID, "prepared", "aborted"); err != nil {
		t.Fatal(err)
	}
	replayed, exists, err := s.PrepareContent(ctx, "admin", valid, "aborted-upload-01")
	if err != nil || !exists || replayed.Status != "aborted" || replayed.ID != p.ID {
		t.Fatal("aborted upload was silently revived", err)
	}
	object := contentPublish(t, s, "admin", valid)
	contentSelect(t, s, object, 0, false)
	// Missing provider entries retain the source but invalidate its last asset.
	// Independent content remains readable without reviving that asset version.
	publishEvidenceTracks(t, s)
	if _, err = s.ContentObject(ctx, "admin", object.ID); err != nil {
		t.Fatal("offline source lost content access", err)
	}
	selection, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || len(selection.Bindings) != 1 || selection.Bindings[0].Current || selection.Bindings[0].State != "stale" || !selection.Bindings[0].ManualLocked {
		t.Fatal("missing asset binding remained current or lost its lock", err)
	}
	_, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: object.Metadata.Target, ObjectID: object.ID, ExpectedRevision: 1}, domain.NewID())
	matchingError(t, err, "RESOURCE_CHANGED")
}

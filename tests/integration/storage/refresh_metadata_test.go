package storage_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func refreshPreview(t *testing.T, s *sqlite.Store, track library.Track, field, body string) (enrichment.Job, enrichment.PreviewCandidate) {
	t.Helper()
	ctx := context.Background()
	job, _, err := s.QueueEnrichment(ctx, "admin", enrichmentRequest(track.ID), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.NewID()
	input := contentInput(track, "und", body)
	if field == "artwork" {
		input.Metadata.Field, input.Metadata.Language, input.Metadata.Format, input.MIME = field, "", "png", "image/png"
		input.Width, input.Height = 64, 64
	}
	input.Metadata.Origin = mediastore.Origin{Kind: "scraper", InstanceID: "scraper", CandidateID: id}
	object := contentPublish(t, s, "admin", input)
	candidate := enrichment.PreviewCandidate{
		CandidateID: id, Target: work.Target, InstanceID: "scraper", Field: field, Language: object.Metadata.Language,
		Format: object.Metadata.Format, MIME: object.MIME, SizeBytes: object.Size, SHA256: object.SHA256,
		Origin: "sidecar", Basis: "exactStem", Status: "available", PreviewObjectID: &object.ID,
		ExpiresAt: time.Now().UTC().Add(enrichment.CandidateLifetime),
	}
	if err = s.ReserveEnrichmentPreview(ctx, work, candidate.SizeBytes); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishEnrichment(ctx, work, enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{candidate}}); err != nil {
		t.Fatal(err)
	}
	return job, candidate
}

func TestRefreshMetadataReplacesAutomaticContentAndKeepsIdenticalBinding(t *testing.T) {
	for _, field := range []string{"artwork", "lyrics"} {
		t.Run(field, func(t *testing.T) {
			s, _, tracks := matchingFixture(t, 1)
			ctx := context.Background()
			guard := func(publish func() error) error { return publish() }
			_, first := refreshPreview(t, s, tracks[0], field, "Initial content")
			selected, err := s.ApplyAutomaticEnrichment(ctx, "admin", first.CandidateID, *first.PreviewObjectID, 0, domain.NewID(), false, guard)
			if err != nil {
				t.Fatal(err)
			}
			_, next := refreshPreview(t, s, tracks[0], field, "Refreshed content")
			_, err = s.ApplyAutomaticEnrichment(ctx, "admin", next.CandidateID, *next.PreviewObjectID, selected.Revision, domain.NewID(), false, guard)
			matchingError(t, err, "CONFLICT")
			_, err = s.ApplyAutomaticEnrichment(ctx, "admin", next.CandidateID, *next.PreviewObjectID, 0, domain.NewID(), true, guard)
			matchingError(t, err, "CONFLICT")
			_, err = s.ApplyAutomaticEnrichment(ctx, "admin", next.CandidateID, *next.PreviewObjectID, selected.Revision, domain.NewID(), true, func(func() error) error {
				return domain.Fail("RESOURCE_CHANGED", "Source changed during publication")
			})
			matchingError(t, err, "RESOURCE_CHANGED")
			refreshed, err := s.ApplyAutomaticEnrichment(ctx, "admin", next.CandidateID, *next.PreviewObjectID, selected.Revision, domain.NewID(), true, guard)
			if err != nil || refreshed.Revision != 2 || refreshed.Bindings[0].ObjectID != *next.PreviewObjectID || refreshed.Bindings[0].ManualLocked {
				t.Fatal("refresh did not replace automatic content", refreshed, err)
			}
			job, same := refreshPreview(t, s, tracks[0], field, "Refreshed content")
			key := domain.NewID()
			_, err = s.ApplyAutomaticEnrichment(ctx, "admin", same.CandidateID, *same.PreviewObjectID, 2, key, true, func(func() error) error { return nil })
			matchingError(t, err, "CONFLICT")
			unchanged, err := s.ApplyAutomaticEnrichment(ctx, "admin", same.CandidateID, *same.PreviewObjectID, 2, key, true, guard)
			if err != nil || !reflect.DeepEqual(refreshed, unchanged) {
				t.Fatal("identical refresh changed binding or revision", unchanged, err)
			}
			if _, err = s.CancelEnrichment(ctx, "admin", job.JobID); err != nil {
				t.Fatal(err)
			}
			called := false
			replay, err := s.ApplyAutomaticEnrichment(ctx, "admin", same.CandidateID, *same.PreviewObjectID, 2, key, true, func(publish func() error) error { called = true; return publish() })
			if err != nil || called || !reflect.DeepEqual(unchanged, replay) {
				t.Fatal("unchanged refresh did not replay", replay, err)
			}
			_, err = s.ApplyAutomaticEnrichment(ctx, "admin", same.CandidateID, *same.PreviewObjectID, 2, key, false, guard)
			matchingError(t, err, "CONFLICT")
			var bindings int
			if err = s.DB.QueryRow("SELECT COUNT(*) FROM media_content_bindings WHERE source_id=?", tracks[0].ID).Scan(&bindings); err != nil || bindings != 2 {
				t.Fatal("identical refresh created another binding", bindings, err)
			}
		})
	}
}

func TestRescanRebindsIdenticalArtworkToNewAsset(t *testing.T) {
	s, observations, tracks := matchingFixture(t, 1)
	ctx := t.Context()
	guard := func(publish func() error) error { return publish() }
	track := tracks[0]
	_, initial := refreshPreview(t, s, track, "artwork", "Same cover bytes")
	before, err := s.ApplyAutomaticEnrichment(ctx, "admin", initial.CandidateID, *initial.PreviewObjectID, 0, domain.NewID(), false, guard)
	if err != nil {
		t.Fatal(err)
	}
	observations[0].ObservedVersion = "new-audio-version"
	publishEvidenceTracks(t, s, observations...)
	updated, err := s.FindTrack(ctx, "admin", track.ID)
	if err != nil || updated.AssetID == track.AssetID {
		t.Fatalf("asset was not replaced: %v", err)
	}
	_, same := refreshPreview(t, s, updated, "artwork", "Same cover bytes")
	after, err := s.ApplyAutomaticEnrichment(ctx, "admin", same.CandidateID, *same.PreviewObjectID, before.Revision, domain.NewID(), false, guard)
	if err != nil || len(after.Bindings) != 1 {
		t.Fatalf("cover binding was not restored: %v", err)
	}
	b := after.Bindings[0]
	if !b.Current || b.ManualLocked || b.ObjectID != *initial.PreviewObjectID || b.Target.AssetID != updated.AssetID || b.ID == before.Bindings[0].ID {
		t.Fatalf("original cover was not rebound to the new asset: %+v", b)
	}
	expectLibraryCovers(t, s, "admin", map[string]string{track.ID: b.ObjectID})
}

func TestRefreshMetadataPreservesSourceManualLocks(t *testing.T) {
	for _, field := range []string{"artwork", "lyrics"} {
		t.Run(field, func(t *testing.T) {
			s, _, tracks := matchingFixture(t, 1)
			ctx := context.Background()
			guard := func(publish func() error) error { return publish() }
			_, first := refreshPreview(t, s, tracks[0], field, "Manually selected content")
			selected, err := s.ApplyEnrichment(ctx, "admin", first.CandidateID, *first.PreviewObjectID, 0, domain.NewID(), guard)
			if err != nil {
				t.Fatal(err)
			}
			_, next := refreshPreview(t, s, tracks[0], field, "Refreshed content")
			_, err = s.ApplyAutomaticEnrichment(ctx, "admin", next.CandidateID, *next.PreviewObjectID, selected.Revision, domain.NewID(), true, guard)
			matchingError(t, err, "CONFLICT")
			current, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
			if err != nil || !reflect.DeepEqual(selected, current) {
				t.Fatal("refresh changed manually locked content", current, err)
			}
		})
	}
}

func TestRefreshMetadataPreservesRecordingManualCoverOnlyForMembers(t *testing.T) {
	for _, member := range []bool{true, false} {
		name := "detached"
		if member {
			name = "member"
		}
		t.Run(name, func(t *testing.T) {
			s, _, tracks := matchingFixture(t, 2)
			ctx := context.Background()
			guard := func(publish func() error) error { return publish() }
			_, first := refreshPreview(t, s, tracks[0], "artwork", "Recording cover")
			selected, err := s.ApplyAutomaticEnrichment(ctx, "admin", first.CandidateID, *first.PreviewObjectID, 0, domain.NewID(), false, guard)
			if err != nil {
				t.Fatal(err)
			}
			recording := recordingCoverGroup(t, s, tracks[0].ID, tracks[1].ID)
			if _, err = s.SelectRecordingCover(ctx, "admin", library.SelectRecordingCoverRequest{RecordingID: recording, ObjectID: first.PreviewObjectID}, domain.NewID()); err != nil {
				t.Fatal(err)
			}
			if !member {
				if _, err = s.Split(ctx, "admin", matching.SplitRequest{ExpectedRevision: 1, RecordingID: recording, SourceTrackIDs: []string{tracks[0].ID}}, domain.NewID()); err != nil {
					t.Fatal(err)
				}
			}
			_, next := refreshPreview(t, s, tracks[0], "artwork", "Refreshed recording cover")
			refreshed, err := s.ApplyAutomaticEnrichment(ctx, "admin", next.CandidateID, *next.PreviewObjectID, selected.Revision, domain.NewID(), true, guard)
			if member {
				matchingError(t, err, "CONFLICT")
				current, readErr := s.ContentSelection(ctx, "admin", tracks[0].ID)
				if readErr != nil || !reflect.DeepEqual(selected, current) {
					t.Fatal("refresh replaced recording manual cover", current, readErr)
				}
			} else if err != nil || refreshed.Bindings[0].ObjectID != *next.PreviewObjectID {
				t.Fatal("unrelated recording preference prevented refresh", refreshed, err)
			}
		})
	}
}

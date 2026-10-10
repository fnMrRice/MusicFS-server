package storage_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func TestEnrichmentAlbumSnapshotReplayAndRestart(t *testing.T) {
	s, items, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	selected := contentPublish(t, s, "admin", contentInput(tracks[0], "und", "selected lyrics"))
	contentSelect(t, s, selected, 0, false)
	a, err := s.CreateSourceAlbum(ctx, "admin", albums.CreateSourceAlbumRequest{ProviderInstanceID: tracks[0].ProviderID, Name: "Exact album", SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, "enrichment-album-create")
	if err != nil {
		t.Fatal(err)
	}
	r := enrichmentRequest()
	r.SourceAlbum = &enrichment.SourceAlbumRequest{SourceAlbumID: a.ID, ExpectedRevision: a.Revision}
	j, replay, err := s.QueueEnrichment(ctx, "admin", r, "enrichment-album-job")
	if err != nil || replay || len(j.Targets) != 2 || j.Request.SourceAlbum == nil || len(j.Request.SourceTrackIDs) != 0 {
		t.Fatal("album queue", err)
	}
	for _, target := range j.Targets {
		if target.Target.SourceTrackID == tracks[0].ID && (target.Target.AssetID != tracks[0].AssetID || target.Target.Revision != 1) {
			t.Fatal("target selection revision not captured")
		}
	}
	a, err = s.UpdateSourceAlbum(ctx, "admin", a.ID, albums.UpdateSourceAlbumRequest{ExpectedRevision: a.Revision, Name: a.Name, SourceTrackIDs: []string{tracks[2].ID}}, "enrichment-album-edit")
	if err != nil {
		t.Fatal(err)
	}
	items[0].ObservedVersion = "v2"
	publishEvidenceTracks(t, s, items...)
	again, replay, err := s.QueueEnrichment(ctx, "admin", r, "enrichment-album-job")
	if err != nil || !replay || !reflect.DeepEqual(j, again) {
		t.Fatal("old album request was reinterpreted", err)
	}
	_, _, err = s.QueueEnrichment(ctx, "admin", r, "enrichment-album-new-key")
	matchingError(t, err, "CONFLICT")
	r.SourceAlbum = &enrichment.SourceAlbumRequest{SourceAlbumID: a.ID, ExpectedRevision: a.Revision}
	_, _, err = s.QueueEnrichment(ctx, "admin", r, "enrichment-album-job")
	matchingError(t, err, "CONFLICT")
	newJob, _, err := s.QueueEnrichment(ctx, "admin", r, "enrichment-album-new-target")
	if err != nil || len(newJob.Targets) != 1 || newJob.Targets[0].Target.SourceTrackID != tracks[2].ID {
		t.Fatal("new album revision not used", err)
	}
	var number int
	var name, path string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&number, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.RecoverEnrichment(ctx); err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.GetEnrichmentJob(ctx, "admin", j.JobID)
	if err != nil || !reflect.DeepEqual(persisted, j) {
		t.Fatal("album target snapshot changed on restart", err)
	}
}

func TestEnrichmentAlbumLimitsAndOwnership(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	items := make([]library.Observation, 65)
	for i := range items {
		items[i] = evidenceObservation(fmt.Sprintf("album-%03d", i), "v1")
	}
	tracks := publishEvidenceTracks(t, s, items...)
	ids := []string{}
	for _, track := range tracks {
		ids = append(ids, track.ID)
	}
	for _, count := range []int{0, 64, 65} {
		a, err := s.CreateSourceAlbum(ctx, "admin", albums.CreateSourceAlbumRequest{ProviderInstanceID: tracks[0].ProviderID, Name: "Bounded", SourceTrackIDs: ids[:count]}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		r := enrichmentRequest()
		r.SourceAlbum = &enrichment.SourceAlbumRequest{SourceAlbumID: a.ID, ExpectedRevision: a.Revision}
		j, _, err := s.QueueEnrichment(ctx, "admin", r, domain.NewID())
		if count == 64 {
			if err != nil || len(j.Targets) != 64 {
				t.Fatal("64-member album rejected", err)
			}
			if _, err = s.CancelEnrichment(ctx, "admin", j.JobID); err != nil {
				t.Fatal(err)
			}
		} else {
			matchingError(t, err, "INVALID_ARGUMENT")
		}
		_, _, err = s.QueueEnrichment(ctx, "other", r, domain.NewID())
		matchingError(t, err, "NOT_FOUND")
		r.SourceTrackIDs = []string{tracks[0].ID}
		_, _, err = s.QueueEnrichment(ctx, "admin", r, domain.NewID())
		matchingError(t, err, "INVALID_ARGUMENT")
	}
}

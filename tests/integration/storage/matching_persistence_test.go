package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/matching"
)

func TestMatchingPersistenceCanonicalReplayAndCancellation(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	ids := []string{tracks[0].ID, tracks[1].ID}
	request := matching.LinkRequest{SourceTrackIDs: ids}
	linked, err := s.ManualLink(ctx, "admin", request, "persistent-link-01")
	if err != nil {
		t.Fatal(err)
	}
	request.SourceTrackIDs = []string{ids[1], ids[0]}
	replay, err := s.ManualLink(ctx, "admin", request, "persistent-link-01")
	if err != nil || !reflect.DeepEqual(replay, linked) {
		t.Fatal("canonical set retry differs", err)
	}
	snapshot := matchingSnapshot(t, s, tracks[0].ID, tracks[2].ID)
	page, err := s.SaveCandidates(ctx, "admin", snapshot, matchingResults(t, snapshot))
	if err != nil || len(page.Items) != 1 {
		t.Fatal("candidate setup failed", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.SaveCandidates(canceled, "admin", snapshot, nil); err == nil {
		t.Fatal("canceled refresh committed")
	}
	if _, err = s.Unlink(canceled, "admin", matching.UnlinkRequest{ExpectedRevision: 1, SourceTrackID: ids[0]}, domain.NewID()); err == nil {
		t.Fatal("canceled manual operation committed")
	}
	var sequence int
	var name, path string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
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
	fresh, err := reopened.Candidates(ctx, "admin", tracks[0].ID, "", 20)
	if err != nil || !reflect.DeepEqual(fresh, page) {
		t.Fatal("candidate evidence/identity lost on restart", err)
	}
	link, err := reopened.GetLink(ctx, "admin", ids[0])
	if err != nil || link.RecordingID == nil || *link.RecordingID != *linked.RecordingID || link.Revision != 1 {
		t.Fatal("manual link lost on restart", err)
	}
	if _, err = reopened.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: 1, SourceTrackID: ids[0]}, "later-unlink-01"); err != nil {
		t.Fatal(err)
	}
	replay, err = reopened.ManualLink(ctx, "admin", request, "persistent-link-01")
	if err != nil || !reflect.DeepEqual(replay, linked) {
		t.Fatal("restart/advanced revision changed prior idempotent result", err)
	}
	link, err = reopened.GetLink(ctx, "admin", ids[0])
	if err != nil || link.RecordingID != nil || link.Revision != 2 {
		t.Fatal("replay resurrected an old association", err)
	}
}

func TestMatchingInvalidSelectionsAndForgedSnapshots(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	for _, ids := range [][]string{nil, {""}, {tracks[0].ID, tracks[0].ID}, make([]string, 257)} {
		_, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, domain.NewID())
		matchingError(t, err, "INVALID_ARGUMENT")
	}
	snapshot := matchingSnapshot(t, s, tracks[0].ID, tracks[1].ID)
	results := matchingResults(t, snapshot)
	forged := snapshot
	forged.Anchor.BindingRevision = "not-current"
	_, err := s.SaveCandidates(ctx, "admin", forged, results)
	matchingError(t, err, "CONFLICT")
	forged = snapshot
	forged.Sources = append(forged.Sources, forged.Sources[0])
	_, err = s.SaveCandidates(ctx, "admin", forged, results)
	matchingError(t, err, "INVALID_ARGUMENT")
	results[0].RequiresConfirmation = false
	_, err = s.SaveCandidates(ctx, "admin", snapshot, results)
	matchingError(t, err, "INVALID_ARGUMENT")
	if rev, _ := s.MatchingRevision(ctx, "admin"); rev != 0 {
		t.Fatal("invalid requests changed revision")
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM matching_candidates").Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid publication partially committed", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.CandidateSnapshot(ctx, "admin", tracks[0].ID, nil, 1); err == nil {
		t.Fatal("canceled snapshot accepted")
	}
}

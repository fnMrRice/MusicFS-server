package storage_test

import (
	"context"
	"fmt"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"testing"
	"time"
)

func waitMatching(t *testing.T, s *sqlite.Store) matching.Run {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r, err := s.MatchingRun(t.Context(), "admin")
		if err != nil {
			t.Fatal(err)
		}
		if r.Job.Status == "succeeded" {
			return r
		}
		if r.Job.Status == "failed" {
			t.Fatalf("matching failed: %+v", r.Job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("matching did not finish")
	return matching.Run{}
}
func newMatchingAutomation(s *sqlite.Store) *matching.Automation {
	return matching.NewAutomation(s, s, matching.New(s, nil, standard.Plugin{}), standard.Detector())
}
func allMatching(t *testing.T, s *sqlite.Store) []matching.Candidate {
	t.Helper()
	var result []matching.Candidate
	after := ""
	for {
		page, err := s.Candidates(t.Context(), "admin", "", after, 17)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, page.Items...)
		if page.NextAfter == nil {
			return result
		}
		after = *page.NextAfter
	}
}
func TestAutomaticMatchingCoversWholeLibraryAndAllBatches(t *testing.T) {
	s := evidenceStore(t)
	items := make([]library.Observation, 946)
	for i := range items {
		items[i] = evidenceObservation(fmt.Sprintf("track-%04d", i), "v1")
		title, duration := fmt.Sprintf("Unique %d", i), int64(180000)
		if i < 23 {
			title = "Many versions"
		}
		if i == 100 || i == 945 {
			title = "Across library pages"
		}
		items[i].Title = &title
		items[i].DurationMs = &duration
		items[i].Artists = []string{"Artist"}
	}
	publishEvidenceTracks(t, s, items...)
	a := newMatchingAutomation(s)
	a.Start(t.Context())
	defer a.Close()
	r := waitMatching(t, s)
	if r.Job.Total != 946 || r.Job.Processed != 946 || r.Job.Candidates != 254 {
		t.Fatalf("incomplete matching: %+v", r.Job)
	}
	found := allMatching(t, s)
	if len(found) != 254 {
		t.Fatalf("lost batched candidates: %d", len(found))
	}
	for _, c := range found {
		if c.LeftTrack == nil || c.RightTrack == nil || c.Score != 35 {
			t.Fatal("missing trusted display or metadata candidates")
		}
		link, err := s.GetLink(t.Context(), "admin", c.Left.SourceTrackID)
		if err != nil || link.RecordingID != nil {
			t.Fatal("automatic matching made manual association", err)
		}
	}
	first := found[0]
	if _, err := s.SetExclusion(t.Context(), "admin", matching.ExclusionRequest{LeftSourceTrackID: first.Left.SourceTrackID, RightSourceTrackID: first.Right.SourceTrackID, Excluded: true}, "exclude-auto"); err != nil {
		t.Fatal(err)
	}
	second := found[1]
	if _, err := s.ManualLink(t.Context(), "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{second.Left.SourceTrackID, second.Right.SourceTrackID}}, "link-auto"); err != nil {
		t.Fatal(err)
	}
	job, err := a.Execute(t.Context(), "admin", "scan", "scan-after-decisions")
	if err != nil || job.Status != "queued" {
		t.Fatal(job, err)
	}
	r = waitMatching(t, s)
	if len(allMatching(t, s)) != 252 {
		t.Fatal("manual decisions were not preserved")
	}
	replay, err := a.Execute(t.Context(), "admin", "scan", "scan-after-decisions")
	if err != nil || replay.ID == nil || *replay.ID != *r.Job.ID || replay.Status != "succeeded" {
		t.Fatal("lost action idempotency", err)
	}
}

func TestMatchingSettingsPersistAndQueuedWorkRecovers(t *testing.T) {
	s, _, _ := matchingFixture(t, 3)
	ctx := context.Background()
	if _, err := s.PutPluginSettings(ctx, "admin", standard.PluginID, 0, settings.Values{"automatic": false, "minimumScore": float64(80)}); err != nil {
		t.Fatal(err)
	}
	a := newMatchingAutomation(s)
	job, err := a.Execute(ctx, "admin", "scan", "persisted-action")
	if err != nil || job.Status != "queued" {
		t.Fatal(job, err)
	}
	b := newMatchingAutomation(s)
	b.Start(t.Context())
	r := waitMatching(t, s)
	b.Close()
	if r.Job.Candidates != 0 || r.Job.ID == nil || *r.Job.ID != *job.ID {
		t.Fatal("recovery ignored persisted settings or job")
	}
	page, err := settings.New(s, newMatchingAutomation(s).Provider()).Read(ctx, "admin", standard.PluginID)
	if err != nil || page.Values["automatic"] != false || page.Revision != 1 {
		t.Fatal("settings did not survive recreation", err)
	}
	other, err := settings.New(s, newMatchingAutomation(s).Provider()).Read(ctx, "other", standard.PluginID)
	if err != nil || other.Revision != 0 || other.Values["automatic"] != true || other.Job.Status != "idle" {
		t.Fatal("private settings or job leaked", err)
	}
	if _, err = s.PutPluginSettings(ctx, "admin", standard.PluginID, 0, settings.Values{}); err == nil {
		t.Fatal("stale settings update accepted")
	}
	c := newMatchingAutomation(s)
	next, err := c.Execute(ctx, "admin", "scan", "second-action-key")
	if err != nil {
		t.Fatal(err)
	}
	c.Start(t.Context())
	waitMatching(t, s)
	c.Close()
	old, err := newMatchingAutomation(s).Execute(ctx, "admin", "scan", "persisted-action")
	if err != nil || old.ID == nil || *old.ID != *job.ID || old.Status != "succeeded" {
		t.Fatal("historical idempotency result was replaced", err)
	}
	latest, err := s.MatchingRun(ctx, "admin")
	if err != nil || latest.Job.ID == nil || *latest.Job.ID != *next.ID {
		t.Fatal("old action key created a third task", err)
	}
}

func TestCandidateDisplayPreservesExistingRecordingMembers(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := t.Context()
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, "existing-recording")
	if err != nil {
		t.Fatal(err)
	}
	a := newMatchingAutomation(s)
	a.Start(ctx)
	defer a.Close()
	waitMatching(t, s)
	found := allMatching(t, s)
	if len(found) != 2 {
		t.Fatal("one-sided existing recording candidates missing")
	}
	for _, c := range found {
		for _, track := range []*library.Track{c.LeftTrack, c.RightTrack} {
			if track.ID == tracks[2].ID {
				continue
			}
			if track.RecordingID == nil || *track.RecordingID != *linked.RecordingID || len(track.Versions) != 2 {
				t.Fatal("candidate would split an existing recording")
			}
		}
	}
}

func TestCandidatePaginationSkipsMoreThanThreePagesOfStaleBindings(t *testing.T) {
	s := evidenceStore(t)
	items := make([]library.Observation, 128)
	for i := range items {
		items[i] = evidenceObservation(fmt.Sprintf("pair-%03d", i), "v1")
		title, duration := fmt.Sprintf("Recording %d", i/2), int64(180000)
		items[i].Title = &title
		items[i].DurationMs = &duration
		items[i].Artists = []string{"Artist"}
	}
	publishEvidenceTracks(t, s, items...)
	a := newMatchingAutomation(s)
	a.Start(t.Context())
	waitMatching(t, s)
	a.Close()
	before := allMatching(t, s)
	if len(before) != 64 {
		t.Fatal("invalid paging fixture")
	}
	changed := map[string]bool{}
	for _, c := range before[:61] {
		changed[c.LeftTrack.DisplayName] = true
	}
	for i := range items {
		if changed[items[i].DisplayName] {
			title := "Changed " + items[i].DisplayName
			items[i].Title = &title
		}
	}
	publishEvidenceTracks(t, s, items...)
	page, err := s.Candidates(t.Context(), "admin", "", "", 2)
	if err != nil || len(page.Items) != 2 || page.NextAfter == nil {
		t.Fatalf("stale rows blocked the first current page: %+v %v", page, err)
	}
	next, err := s.Candidates(t.Context(), "admin", "", *page.NextAfter, 2)
	if err != nil || len(next.Items) != 1 || next.NextAfter != nil {
		t.Fatal("current candidate page lost after stale rows", err)
	}
	if page.Items[0].ID != before[61].ID || next.Items[0].ID != before[63].ID {
		t.Fatal("candidate order changed")
	}
}

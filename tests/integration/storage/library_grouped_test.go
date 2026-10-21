package storage_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestGroupedLibraryCompleteMembersAcrossPagesAndMatchingSnapshots(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	observations := make([]library.Observation, 260)
	for i := range observations {
		observations[i] = evidenceObservation(fmt.Sprintf("track-%03d", i), "v1")
	}
	publishEvidenceTracks(t, s, observations...)
	first, err := s.ListTracks(ctx, "admin", "local", "", 200)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("raw pagination unavailable: %+v %v", first, err)
	}
	tail, err := s.ListTracks(ctx, "admin", "local", *first.NextCursor, 200)
	if err != nil || len(tail.Items) != 60 {
		t.Fatalf("raw tail unavailable: %+v %v", tail, err)
	}
	ids := []string{first.Items[0].ID, tail.Items[59].ID}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, "grouped-link-pair")
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.ListGroupedTracks(ctx, "admin", "", "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.MatchingRevision == nil || *page.MatchingRevision != 1 {
		t.Fatalf("grouped first page invalid: %+v %v", page, err)
	}
	group := page.Items[0]
	if group.RecordingID == nil || *group.RecordingID != *linked.RecordingID || len(group.Versions) != 2 || group.Versions[0].ID != ids[0] || group.Versions[1].ID != ids[1] {
		t.Fatalf("members outside raw page were lost: %+v", group)
	}
	if _, err = s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: 1, SourceTrackID: ids[1]}, "grouped-unlink-tail"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{ids[0]: true, ids[1]: true}
	groups := 1
	for page.NextCursor != nil {
		next, err := s.ListGroupedTracks(ctx, "admin", "", *page.NextCursor, 200)
		if err != nil || next.LibraryRevision != page.LibraryRevision || next.MatchingRevision == nil || *next.MatchingRevision != 1 {
			t.Fatalf("manual change mixed snapshot revisions: %+v %v", next, err)
		}
		for _, item := range next.Items {
			groups++
			if len(item.Versions) != 1 || item.RecordingID != nil {
				t.Fatalf("unexpected recording or incomplete unlinked item: %+v", item)
			}
			for _, member := range item.Versions {
				if seen[member.ID] || len(member.Versions) != 0 {
					t.Fatalf("member repeated or recursively nested: %+v", member)
				}
				seen[member.ID] = true
			}
		}
		page = next
	}
	if groups != 259 || len(seen) != 260 {
		t.Fatalf("group pagination lost sources: %d groups %d sources", groups, len(seen))
	}
	current, err := s.ListGroupedTracks(ctx, "admin", "", "", 1)
	if err != nil || current.MatchingRevision == nil || *current.MatchingRevision != 2 || len(current.Items[0].Versions) != 1 {
		t.Fatalf("new snapshot did not reflect unlink: %+v %v", current, err)
	}
}

func TestGroupedLibraryProviderFilterPreservesAllOwnedVersions(t *testing.T) {
	s, observations, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	other := groupedPublish(t, s, "admin", "other-provider", evidenceObservation("other-version", "v1"))
	private := matchingPrivateTrack(t, s)
	_, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, other.ID}}, "grouped-cross-source")
	if err != nil {
		t.Fatal(err)
	}
	// 本地版本消失后仍保留人工关联，代表曲目应选择可用的另一来源。
	publishEvidenceTracks(t, s, observations[1])
	page, err := s.ListGroupedTracks(ctx, "admin", "local", "", 200)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("provider-filtered groups invalid: %+v %v", page, err)
	}
	found := false
	for _, item := range page.Items {
		if item.RecordingID == nil {
			continue
		}
		found = true
		if item.ID != other.ID || item.Availability != "available" || len(item.Versions) != 2 {
			t.Fatalf("available representative or cross-provider members lost: %+v", item)
		}
		for _, member := range item.Versions {
			if member.ID == private.ID {
				t.Fatal("another user's source leaked into grouped library")
			}
		}
	}
	if !found {
		t.Fatal("recording missing from matching provider filter")
	}
	filtered, err := s.ListGroupedTracks(ctx, "admin", "other-provider", "", 200)
	if err != nil || len(filtered.Items) != 1 || len(filtered.Items[0].Versions) != 2 {
		t.Fatalf("other provider lost group members: %+v %v", filtered, err)
	}
	bob, err := s.ListGroupedTracks(ctx, "bob", "", "", 200)
	if err != nil || len(bob.Items) != 1 || bob.Items[0].ID != private.ID || bob.MatchingRevision == nil || *bob.MatchingRevision != 0 {
		t.Fatalf("owner isolation failed: %+v %v", bob, err)
	}
}

func TestGroupedLibraryCursorBindingRetryAndExpiry(t *testing.T) {
	s, _, _ := matchingFixture(t, 3)
	ctx := context.Background()
	first, err := s.ListGroupedTracks(ctx, "admin", "local", "", 1)
	if err != nil || first.NextCursor == nil {
		t.Fatal(err)
	}
	cursor := *first.NextCursor
	second, err := s.ListGroupedTracks(ctx, "admin", "local", cursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.ListGroupedTracks(ctx, "admin", "local", cursor, 1)
	if err != nil || !reflect.DeepEqual(second, retry) {
		t.Fatalf("cursor retry drifted: %+v %+v %v", second, retry, err)
	}
	_, err = s.ListGroupedTracks(ctx, "bob", "local", cursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	_, err = s.ListGroupedTracks(ctx, "admin", "", cursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	_, err = s.ListTracks(ctx, "admin", "local", cursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	raw, err := s.ListTracks(ctx, "admin", "local", "", 1)
	if err != nil || raw.NextCursor == nil {
		t.Fatal(err)
	}
	_, err = s.ListGroupedTracks(ctx, "admin", "local", *raw.NextCursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	if _, err = s.DB.Exec("UPDATE library_snapshots SET expires_at='2000-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	_, err = s.ListGroupedTracks(ctx, "admin", "local", cursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
}

func groupedPublish(t *testing.T, store *sqlite.Store, owner, provider string, observation library.Observation) library.Track {
	t.Helper()
	ctx := context.Background()
	if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan("music"), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{observation})); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, owner, provider, "", 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("missing provider fixture: %+v %v", page, err)
	}
	return page.Items[0]
}

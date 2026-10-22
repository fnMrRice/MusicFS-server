package workpackages_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPackageImportReusesScannedSourcesWithoutChangingUnrelatedTracks(t *testing.T) {
	store, work, _ := fixture(t)
	input := publication(work.ID)
	outside := input.Observations[0]
	outside.ExternalID, outside.RelativePath, outside.DisplayName = "outside", "other/track.wav", "track.wav"
	if _, _, err := store.Enqueue(t.Context(), "alice", "local", testkit.ScanPlan("music"), "scan-before-package"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Publish(t.Context(), job, testkit.ScanPublication(job, []library.Observation{input.Observations[0], outside})); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(t.Context(), job, 2, nil); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListTracks(t.Context(), "alice", "local", "", 100)
	if err != nil || len(before.Items) != 2 {
		t.Fatal("scan fixture failed", err)
	}
	p := publish(t, store, "alice", input)
	audio := nodeAt(t, p, "WAV/01.wav")
	matched := false
	for _, track := range before.Items {
		if track.ID == audio.SourceTrackID {
			matched = true
			if track.AssetID != audio.AssetID {
				t.Fatal("unchanged audio received a new asset")
			}
		}
		current, err := store.FindTrack(t.Context(), "alice", track.ID)
		if err != nil || current.Availability != "available" {
			t.Fatal("import changed an unrelated source", err)
		}
	}
	if !matched {
		t.Fatal("package import duplicated an existing source identity")
	}
	after, err := store.ListTracks(t.Context(), "alice", "local", "", 100)
	if err != nil || len(after.Items) != 1 || after.Items[0].ID == audio.SourceTrackID {
		t.Fatal("ordinary music browse retained an ASMR source", err)
	}
	var count int
	if err = store.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM tracks WHERE owner=? AND provider=?", "alice", "local").Scan(&count); err != nil || count != 2 {
		t.Fatal("package import changed stored source count", err)
	}
}

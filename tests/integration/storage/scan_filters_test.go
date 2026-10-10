package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestFilteredScanRequiresHostEvidenceAndPreservesSkippedTracks(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	plan := testkit.ScanPlan("a")
	publishScope(t, s, plan, scopeObservation("visible", "a", "visible.wav"), scopeObservation("dot", "a", ".hidden.wav"), scopeObservation("hidden", "a", "private/song.wav"), scopeObservation("marked", "a", "album/song.wav"))
	before := tracksByName(t, s)
	plan.Options.SkipHidden, plan.Options.RespectNoMedia = true, true
	if _, _, err := s.Enqueue(ctx, "admin", "local", plan, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	publication := testkit.ScanPublication(j, nil)
	if s.Publish(ctx, j, publication) == nil {
		t.Fatal("filtered publication without host evidence accepted")
	}
	publication.ExcludedPaths = map[string][]string{"a": {"private", "album"}}
	for _, path := range []string{"private/song.wav", "album/song.wav", ".hidden.wav"} {
		forged := publication
		forged.Observations = []library.Observation{scopeObservation("forged", "a", path)}
		if s.Publish(ctx, j, forged) == nil {
			t.Fatal("excluded observation accepted", path)
		}
	}
	if err := s.Publish(ctx, j, publication); err != nil {
		t.Fatal(err)
	}
	after := tracksByName(t, s)
	if after["visible"].Availability != "missing" {
		t.Fatal("covered missing file did not reconcile")
	}
	for _, name := range []string{"dot", "hidden", "marked"} {
		if !reflect.DeepEqual(before[name], after[name]) {
			t.Fatal("skipped source or asset was changed", name)
		}
	}
}

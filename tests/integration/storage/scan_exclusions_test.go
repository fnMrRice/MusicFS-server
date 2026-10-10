package storage_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanExclusionsRetainEvidenceAndRejectForgedPublication(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	included := scopeObservation("visible", "root", "track.mp3")
	excluded := scopeObservation("hidden", "root", "private/track.mp3")
	plan := testkit.ScanPlan("root")
	publishScope(t, s, plan, included, excluded)
	before := tracksByName(t, s)
	ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(before["hidden"].Asset))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
		t.Fatal(err)
	}
	before = tracksByName(t, s)
	plan.Options.ExcludePatterns = []string{"private"}
	if _, _, err = s.Enqueue(ctx, "admin", "local", plan, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	job, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{excluded})); err == nil {
		t.Fatal("excluded observation accepted")
	}
	if !reflect.DeepEqual(before, tracksByName(t, s)) {
		t.Fatal("rejected publication changed visible data")
	}
	if err = s.Publish(ctx, job, testkit.ScanPublication(job, nil)); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, job, 0, nil); err != nil {
		t.Fatal(err)
	}
	after := tracksByName(t, s)
	if !reflect.DeepEqual(before["hidden"], after["hidden"]) {
		t.Fatal("excluded track or verified evidence invalidated")
	}
	if after["visible"].Availability != "missing" {
		t.Fatal("included missing path was not reconciled")
	}
	plan.Options.ExcludePatterns = []string{}
	publishScope(t, s, plan, included)
	after = tracksByName(t, s)
	if after["hidden"].Availability != "missing" || after["hidden"].ID != before["hidden"].ID {
		t.Fatal("removing exclusion failed to reconcile same source identity")
	}
}

func TestScanExclusionsRejectRebindingExistingIdentityOutsideScope(t *testing.T) {
	for name, restrict := range map[string]func(*pluginproto.ScanPlan){
		"excluded":        func(p *pluginproto.ScanPlan) { p.Options.ExcludePatterns = []string{"private"} },
		"depth":           func(p *pluginproto.ScanPlan) { p.Options.Recursive, p.Options.MaxDepth = false, 0 },
		"unselected-root": func(p *pluginproto.ScanPlan) { p.Roots = p.Roots[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			s := evidenceStore(t)
			ctx := context.Background()
			plan := testkit.ScanPlan("a", "b")
			publishScope(t, s, plan, scopeObservation("hidden", "a", "private/hidden.mp3"), scopeObservation("visible", "b", "visible.mp3"))
			before := tracksByName(t, s)
			ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(before["hidden"].Asset))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
				t.Fatal(err)
			}
			beforePage, err := s.ListTracks(ctx, "admin", "local", "", 200)
			if err != nil {
				t.Fatal(err)
			}
			beforeHidden, err := s.FindTrack(ctx, "admin", before["hidden"].ID)
			if err != nil {
				t.Fatal(err)
			}
			restrict(&plan)
			if _, _, err = s.Enqueue(ctx, "admin", "local", plan, domain.NewID()); err != nil {
				t.Fatal(err)
			}
			job, err := s.Claim(ctx)
			if err != nil {
				t.Fatal(err)
			}
			forged := scopeObservation("hidden", "b", "allowed.mp3")
			forged.ObservedVersion = "forged-version"
			// A valid earlier observation must also roll back when the later identity is rejected.
			publication := testkit.ScanPublication(job, []library.Observation{scopeObservation("new", "b", "new.mp3"), forged})
			var failure *domain.Error
			if err = s.Publish(ctx, job, publication); !errors.As(err, &failure) || failure.Code != "ACCESS_DENIED" {
				t.Fatalf("rebind error: %v", err)
			}
			afterPage, err := s.ListTracks(ctx, "admin", "local", "", 200)
			if err != nil || !reflect.DeepEqual(beforePage, afterPage) {
				t.Fatal("rejected rebind changed tracks, evidence or library revision", err)
			}
			afterHidden, err := s.FindTrack(ctx, "admin", before["hidden"].ID)
			if err != nil || !reflect.DeepEqual(beforeHidden, afterHidden) {
				t.Fatal("rejected rebind changed private source location", err)
			}
		})
	}
}

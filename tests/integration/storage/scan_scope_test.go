package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func publishScope(t *testing.T, s *sqlite.Store, plan pluginproto.ScanPlan, items ...library.Observation) jobs.Job {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.Enqueue(ctx, "admin", "local", plan, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, items)); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, j, len(items), nil); err != nil {
		t.Fatal(err)
	}
	return j
}

func scopeObservation(id, root, path string) library.Observation {
	o := evidenceObservation(id, "v1")
	o.RootID, o.RelativePath = root, path
	return o
}

func tracksByName(t *testing.T, s *sqlite.Store) map[string]library.Track {
	t.Helper()
	page, err := s.ListTracks(context.Background(), "admin", "local", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]library.Track{}
	for _, item := range page.Items {
		result[item.DisplayName] = item
	}
	return result
}

func TestShallowScanReconcilesOnlyCompletedRootsAndDepth(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	root := scopeObservation("root", "a", "根 文件.mp3")
	child := scopeObservation("child", "a", "子目录/child.mp3")
	deep := scopeObservation("deep", "a", "子目录/深层/deep.mp3")
	other := scopeObservation("other", "b", "other.mp3")
	full := testkit.ScanPlan("a", "b")
	publishScope(t, s, full, root, child, deep, other)
	before := tracksByName(t, s)
	for _, name := range []string{"child", "deep", "other"} {
		a := before[name].Asset
		ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(a))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
			t.Fatal(err)
		}
	}
	before = tracksByName(t, s)
	snapshot, err := s.ListTracks(ctx, "admin", "local", "", 1)
	if err != nil || snapshot.NextCursor == nil {
		t.Fatal("snapshot missing", err)
	}
	flat := testkit.ScanPlan("a")
	flat.Options = pluginproto.ImportOptions{}
	publishScope(t, s, flat)
	after := tracksByName(t, s)
	if after["root"].Availability != "missing" || after["root"].Asset.Current {
		t.Fatal("covered missing file retained as current")
	}
	for _, name := range []string{"child", "deep", "other"} {
		if !reflect.DeepEqual(before[name], after[name]) {
			t.Fatalf("out-of-scope track or evidence changed: %s", name)
		}
	}
	continued, err := s.ListTracks(ctx, "admin", "local", *snapshot.NextCursor, 200)
	if err != nil || continued.LibraryRevision != snapshot.LibraryRevision {
		t.Fatal("scan rewrote existing list snapshot", err)
	}
	for _, track := range continued.Items {
		if track.Availability != "available" || !track.Asset.Current {
			t.Fatal("prior list snapshot was mutated")
		}
	}
	one := testkit.ScanPlan("a")
	one.Options.MaxDepth = 1
	publishScope(t, s, one, root)
	after = tracksByName(t, s)
	if after["root"].ID != before["root"].ID || after["root"].Availability != "available" || after["child"].Availability != "missing" {
		t.Fatal("depth-one reconciliation lost source identity or missed child")
	}
	for _, name := range []string{"deep", "other"} {
		if !reflect.DeepEqual(before[name], after[name]) {
			t.Fatalf("depth-one scan changed excluded track %s", name)
		}
	}
	publishScope(t, s, full, root)
	after = tracksByName(t, s)
	for _, name := range []string{"child", "deep", "other"} {
		if after[name].Availability != "missing" || after[name].Asset.Current {
			t.Fatalf("full scan did not reconcile %s", name)
		}
	}
}

func TestScanPublicationRejectsForgedPlanAndPartialScopeAtomically(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	publishScope(t, s, testkit.ScanPlan("a", "b"), scopeObservation("old", "a", "old.mp3"))
	before, err := s.ListTracks(ctx, "admin", "local", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	plan := testkit.ScanPlan("a", "b")
	plan.Options = pluginproto.ImportOptions{}
	if _, _, err = s.Enqueue(ctx, "admin", "local", plan, "scope-security"); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*jobs.Job, *library.ScanPublication){
		"missing-root":   func(_ *jobs.Job, p *library.ScanPublication) { p.CompletedRoots = p.CompletedRoots[:1] },
		"duplicate-root": func(_ *jobs.Job, p *library.ScanPublication) { p.CompletedRoots[1] = p.CompletedRoots[0] },
		"root-identity":  func(_ *jobs.Job, p *library.ScanPublication) { p.CompletedRoots[0].RootIdentity = "replaced" },
		"digest":         func(_ *jobs.Job, p *library.ScanPublication) { p.PlanDigest = "wrong" },
		"job-plan": func(j *jobs.Job, _ *library.ScanPublication) {
			j.Plan.Options = pluginproto.ImportOptions{Recursive: true, MaxDepth: 64}
		},
		"job-target":     func(j *jobs.Job, _ *library.ScanPublication) { j.ProviderInstanceID = "other" },
		"job-owner":      func(j *jobs.Job, _ *library.ScanPublication) { j.Owner = "other" },
		"job-generation": func(j *jobs.Job, _ *library.ScanPublication) { j.Generation++ },
		"unselected-root": func(_ *jobs.Job, p *library.ScanPublication) {
			p.Observations = append(p.Observations, scopeObservation("bad", "c", "bad.mp3"))
		},
		"too-deep": func(_ *jobs.Job, p *library.ScanPublication) {
			p.Observations = append(p.Observations, scopeObservation("bad", "a", "sub/bad.mp3"))
		},
		"traversal": func(_ *jobs.Job, p *library.ScanPublication) {
			p.Observations = append(p.Observations, scopeObservation("bad", "a", "../bad.mp3"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := j
			candidate.Plan.Roots = append([]pluginproto.ScanRoot(nil), j.Plan.Roots...)
			publication := testkit.ScanPublication(j, []library.Observation{scopeObservation("new", "a", "new.mp3")})
			mutate(&candidate, &publication)
			if err := s.Publish(ctx, candidate, publication); err == nil {
				t.Fatal("invalid publication accepted")
			}
			after, err := s.ListTracks(ctx, "admin", "local", "", 200)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected publication partially changed library", err)
			}
		})
	}
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, nil)); err != nil {
		t.Fatal("valid scope rejected after failed attempts", err)
	}
}

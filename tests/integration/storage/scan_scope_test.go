package storage_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
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

func TestUnlimitedDepthReconcilesDeepTracksOnlyWhenCovered(t *testing.T) {
	s := evidenceStore(t)
	deep := scopeObservation("deep", "a", strings.Repeat("d/", 70)+"song.mp3")
	full := testkit.ScanPlan("a")
	full.Options.MaxDepth = 0
	publishScope(t, s, full, deep)
	before := tracksByName(t, s)["deep"]
	if before.Availability != "available" {
		t.Fatal("unlimited scan did not publish deep track")
	}
	for _, options := range []pluginproto.ImportOptions{
		{Recursive: true, MaxDepth: 64},
		{},
		{Recursive: true, ExcludePatterns: []string{"d"}},
	} {
		limited := full
		limited.Options = options
		publishScope(t, s, limited)
		if after := tracksByName(t, s)["deep"]; !reflect.DeepEqual(before, after) {
			t.Fatal("unvisited deep track was changed by a limited scan")
		}
	}
	publishScope(t, s, full)
	if after := tracksByName(t, s)["deep"]; after.Availability != "missing" || after.Asset.Current {
		t.Fatal("unlimited scan did not reconcile a missing deep track")
	}
}

func scopeObservation(id, root, path string) library.Observation {
	o := evidenceObservation(id, "v1")
	o.RootID, o.RelativePath = root, path
	return o
}

func TestStablePathReplacementPreservesSourcesAcrossRenames(t *testing.T) {
	store := evidenceStore(t)
	plan := testkit.ScanPlan("a", "b")
	original := scopeObservation("original", "a", "track.mp3")
	other := scopeObservation("other-root", "b", "track.mp3")
	publishScope(t, store, plan, original, other)
	before := tracksByName(t, store)

	// 原文件移动后在旧路径补入同内容的新文件；另一个根的同名文件直接原位替换。
	newFile := scopeObservation("new-file", "a", "track.mp3")
	original.RelativePath = "renamed.mp3"
	replaced := scopeObservation("replaced", "b", "track.mp3")
	if _, _, err := store.Enqueue(t.Context(), "admin", "local", plan, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	publication := testkit.ScanPublication(job, []library.Observation{newFile, replaced, original})
	publication.StablePaths = true
	if err = store.Publish(t.Context(), job, publication); err != nil {
		t.Fatal(err)
	}
	after := tracksByName(t, store)
	if len(after) != 3 || after["new-file"].ID == before["original"].ID || after["new-file"].ID == before["other-root"].ID {
		t.Fatal("the new file took over an existing source", after)
	}
	if after["original"].ID != before["original"].ID || after["original"].AssetID != before["original"].AssetID {
		t.Fatal("renaming changed the original track or asset", after["original"])
	}
	if after["replaced"].ID != before["other-root"].ID || after["replaced"].AssetID != before["other-root"].AssetID {
		t.Fatal("replacing the other root's file with identical content changed its track or asset", after["replaced"])
	}
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

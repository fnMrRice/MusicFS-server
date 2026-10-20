package enrichment_test

import (
	"context"
	"crypto/sha512"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func automaticService(t *testing.T, f *fixture, policy enrichment.AutomaticPolicy, runtimes []enrichmentworker.Runtime) {
	t.Helper()
	f.service.Close()
	service, err := enrichmentworker.New(f.store, f.lib, f.content, runtimes, enrichmentworker.Config{Automatic: policy})
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	t.Cleanup(service.Close)
	if err = service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func rescanAutomatic(t *testing.T, f fixture, lyrics bool) jobs.Job {
	t.Helper()
	ctx := context.Background()
	p := testkit.ScanPlan("root")
	p.Options.ImportLyrics = lyrics
	if _, _, err := f.store.Enqueue(ctx, "admin", "local", p, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := f.store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: f.root}})
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for _, name := range []string{"good.mp3", "other.mp3"} {
		e, err := access.Inspect("root", name)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: "root", RelativePath: name, ObservedVersion: e.ObservedVersion, SizeBytes: e.SizeBytes, MIME: e.MIME, DisplayName: name}})
	}
	if err = f.store.Publish(ctx, j, testkit.ScanPublication(j, items)); err != nil {
		t.Fatal(err)
	}
	if err = f.store.Finish(ctx, j, len(items), nil); err != nil {
		t.Fatal(err)
	}
	return j
}

func automaticFinished(t *testing.T, f fixture, scan string) []enrichment.AutomationResult {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		page, err := f.service.ScanResults(context.Background(), "admin", scan, "", 200)
		if err != nil {
			t.Fatal(err)
		}
		ready := len(page.Items) > 0
		for _, r := range page.Items {
			if r.State == "queued" || r.State == "preparing" || r.State == "running" {
				ready = false
			}
		}
		if ready {
			return page.Items
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("automatic metadata did not finish")
	return nil
}

func TestAutomaticExplicitImportAllLanguagesAndManualLockAcrossRestart(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	before := map[string][64]byte{}
	for _, name := range []string{"good.mp3", "other.mp3", "good.lrc", "cover.png"} {
		body, err := os.ReadFile(filepath.Join(f.root, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = sha512.Sum512(body)
	}
	// The request remains durable while no worker is running. Global languages
	// cannot suppress English/unknown language explicitly imported by the scan.
	j := rescanAutomatic(t, f, true)
	policy := enrichment.AutomaticPolicy{Languages: []string{"zh"}, InstancePriority: []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}}
	automaticService(t, &f, policy, f.runtimes)
	results := automaticFinished(t, f, j.ID)
	for _, r := range results {
		if r.Reason == "automatic" && (r.State != "skipped" || r.JobID != nil) {
			t.Fatal("disabled global policy ran", r)
		}
	}
	selection, err := f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Bindings) != 2 {
		t.Fatalf("explicit import languages: %+v", selection)
	}
	var english mediastore.Binding
	for _, b := range selection.Bindings {
		if b.Field != "lyrics" || b.ManualLocked {
			t.Fatal("automatic selection was manual or artwork", b)
		}
		if b.Language == "en" {
			english = b
		}
	}
	if english.ID == "" {
		t.Fatal("English alias not imported")
	}
	manual, err := f.content.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: selection.Target, ObjectID: english.ObjectID, ExpectedRevision: selection.Revision}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	f.service.Close()
	next := rescanAutomatic(t, f, true)
	automaticService(t, &f, policy, f.runtimes)
	again := automaticFinished(t, f, next.ID)
	locked := false
	for _, r := range again {
		for _, a := range r.Actions {
			if r.SourceTrackID == f.good().ID && a.Language == "en" && a.Reason == "manualLocked" {
				locked = true
			}
		}
	}
	if !locked {
		t.Fatal("manual lock was not reported")
	}
	current, err := f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil || current.Revision != manual.Revision {
		t.Fatal("repeat import replaced content", current, err)
	}
	for name, digest := range before {
		body, err := os.ReadFile(filepath.Join(f.root, name))
		if err != nil || sha512.Sum512(body) != digest {
			t.Fatal("original changed", name, err)
		}
	}
}

func TestAutomaticRejectsAmbiguousEmbeddedAndDirectoryCover(t *testing.T) {
	f := setup(t)
	body := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00First synthetic lyric")}, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00Different synthetic lyric")})
	if err := os.WriteFile(filepath.Join(f.root, "good.mp3"), body, 0600); err != nil {
		t.Fatal(err)
	}
	j := rescanAutomatic(t, f, false)
	// Start after publication so the policy is captured once at dispatch.
	automaticService(t, &f, enrichment.AutomaticPolicy{Enabled: true, InstancePriority: []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}, Fields: []string{"lyrics", "artwork"}}, f.runtimes)
	results := automaticFinished(t, f, j.ID)
	ambiguous, cover := false, false
	for _, r := range results {
		if r.SourceTrackID != f.good().ID {
			continue
		}
		for _, a := range r.Actions {
			if a.Language == "en" && a.Reason == "ambiguous" {
				ambiguous = true
			}
			if a.Field == "artwork" && a.Reason == "noEligibleCandidate" {
				cover = true
			}
		}
	}
	if !ambiguous || !cover {
		t.Fatalf("unsafe evidence selected: %+v", results)
	}
	selection, err := f.content.ContentSelection(context.Background(), "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Bindings) != 1 || selection.Bindings[0].Language != "und" {
		t.Fatal("ambiguous or generic content selected", selection)
	}
}

func TestAutomaticImportOfflineDependencyIsExplicit(t *testing.T) {
	f := setup(t)
	j := rescanAutomatic(t, f, true)
	automaticService(t, &f, enrichment.AutomaticPolicy{InstancePriority: []string{"disabled"}}, f.runtimes)
	for _, r := range automaticFinished(t, f, j.ID) {
		if r.Reason == "importLyrics" && (r.State != "failed" || r.FailureCode == nil || *r.FailureCode != "SOURCE_OFFLINE") {
			t.Fatal("offline import appeared successful", r)
		}
	}
	selection, err := f.content.ContentSelection(context.Background(), "admin", f.good().ID)
	if err != nil || len(selection.Bindings) != 0 {
		t.Fatal(selection, err)
	}
	f.service.Close()
	// Default priority must not silently omit an enabled runtime whose launch
	// failed, then appear successful using only the lower-priority runtime.
	f.runtimes[1].Enabled = true
	next := rescanAutomatic(t, f, true)
	automaticService(t, &f, enrichment.AutomaticPolicy{}, f.runtimes)
	for _, r := range automaticFinished(t, f, next.ID) {
		if r.Reason == "importLyrics" && (r.State != "failed" || r.FailureCode == nil || *r.FailureCode != "SOURCE_OFFLINE") {
			t.Fatal("failed enabled runtime was silently omitted", r)
		}
	}
}

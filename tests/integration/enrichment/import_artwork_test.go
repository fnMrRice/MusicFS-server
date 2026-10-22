package enrichment_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestImportArtworkStoresEmbeddedAndExactSidecarPreservingManualSelection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	image := testkit.ScraperPNG()
	frame := append([]byte("\x03image/png\x00\x03\x00"), image...)
	audio := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "APIC", Body: frame})
	if err := os.WriteFile(filepath.Join(f.root, "good.mp3"), audio, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "other.png"), image, 0600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(audio)
	options := testkit.ScanPlan("root").Options
	options.ImportArtwork = true
	scan := rescanAutomaticOptions(t, f, options)
	policy := enrichment.AutomaticPolicy{Fields: []string{"lyrics"}, Languages: []string{"zh"}}
	automaticService(t, &f, policy, f.runtimes)
	results := automaticFinished(t, f, scan.ID)
	imported := 0
	for _, result := range results {
		if result.Reason != "importArtwork" {
			continue
		}
		if result.State != "completed" || len(result.Actions) != 1 || result.Actions[0].Field != "artwork" || result.Actions[0].Status != "applied" {
			t.Fatal("artwork was not automatically selected", result)
		}
		imported++
		selection, err := f.content.ContentSelection(ctx, "admin", result.SourceTrackID)
		if err != nil || len(selection.Bindings) != 1 || selection.Bindings[0].ManualLocked {
			t.Fatal(selection, err)
		}
		object, reader, err := f.content.Open(ctx, "admin", selection.Bindings[0].ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(body, image) || object.State != "available" {
			t.Fatal("stored artwork differs from source", err)
		}
	}
	if imported != 2 {
		t.Fatal("missing import triggers", results)
	}
	selection, err := f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := f.content.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: selection.Target, ObjectID: selection.Bindings[0].ObjectID, ExpectedRevision: selection.Revision}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	f.service.Close()
	next := rescanAutomaticOptions(t, f, options)
	automaticService(t, &f, policy, f.runtimes)
	locked, existing := false, false
	for _, result := range automaticFinished(t, f, next.ID) {
		if result.Reason == "importArtwork" {
			for _, action := range result.Actions {
				locked = locked || result.SourceTrackID == f.good().ID && action.Reason == "manualLocked"
				existing = existing || result.SourceTrackID != f.good().ID && action.Reason == "existingContent"
			}
		}
	}
	current, err := f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil || current.Revision != manual.Revision || !locked || !existing {
		t.Fatal("repeated import replaced selected artwork", current, err)
	}
	unchanged, err := os.ReadFile(filepath.Join(f.root, "good.mp3"))
	if err != nil || sha256.Sum256(unchanged) != before {
		t.Fatal("artwork import modified original audio", err)
	}
}

func TestImportArtworkReportsMissingScraperAndRejectsDirectoryCover(t *testing.T) {
	f := setup(t)
	options := testkit.ScanPlan("root").Options
	options.ImportArtwork = true
	scan := rescanAutomaticOptions(t, f, options)
	automaticService(t, &f, enrichment.AutomaticPolicy{}, nil)
	failed := 0
	for _, result := range automaticFinished(t, f, scan.ID) {
		if result.Reason == "importArtwork" {
			if result.State != "failed" || result.FailureCode == nil || *result.FailureCode != "UNSUPPORTED" {
				t.Fatal("missing scraper was not reported", result)
			}
			failed++
		}
	}
	if failed != 2 {
		t.Fatal("missing failure records", failed)
	}
	f.service.Close()
	next := rescanAutomaticOptions(t, f, options)
	automaticService(t, &f, enrichment.AutomaticPolicy{}, f.runtimes)
	for _, result := range automaticFinished(t, f, next.ID) {
		if result.Reason == "importArtwork" {
			if result.State != "completed" || len(result.Actions) != 1 || result.Actions[0].Reason != "noEligibleCandidate" {
				t.Fatal("generic directory cover was selected", result)
			}
		}
	}
	selection, err := f.content.ContentSelection(context.Background(), "admin", f.good().ID)
	if err != nil || len(selection.Bindings) != 0 {
		t.Fatal(selection, err)
	}
	f.service.Close()
	options.ImportArtwork = false
	disabled := rescanAutomaticOptions(t, f, options)
	automaticService(t, &f, enrichment.AutomaticPolicy{}, f.runtimes)
	for _, result := range automaticFinished(t, f, disabled.ID) {
		if result.Reason == "importArtwork" {
			t.Fatal("disabled artwork import created a task", result)
		}
	}
}

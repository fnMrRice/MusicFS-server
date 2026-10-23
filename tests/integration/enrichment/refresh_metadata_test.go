package enrichment_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func refreshMetadataScan(t *testing.T, f *fixture) []enrichment.AutomationResult {
	t.Helper()
	plan := testkit.ScanPlan("root")
	plan.Mode, plan.RefreshMetadata = "full", true
	f.service.Close()
	scan := rescanAutomaticPlan(t, *f, plan)
	// 显式刷新必须覆盖全局禁用、字段和语言过滤。
	automaticService(t, f, enrichment.AutomaticPolicy{Fields: []string{"lyrics"}, Languages: []string{"zh"}}, f.runtimes)
	results := automaticFinished(t, *f, scan.ID)
	if len(results) != 2 {
		t.Fatal("refresh created duplicate automatic jobs", results)
	}
	for _, result := range results {
		if result.Reason != "refreshMetadata" || result.State != "completed" {
			t.Fatal("refresh metadata task did not complete", result)
		}
	}
	return results
}

func selectedRefreshSlot(t *testing.T, f fixture, field, language string) mediastore.Binding {
	t.Helper()
	selection, err := f.content.ContentSelection(context.Background(), "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range selection.Bindings {
		if binding.Field == field && binding.Language == language {
			return binding
		}
	}
	t.Fatalf("selected slot missing: %s/%s", field, language)
	return mediastore.Binding{}
}

func TestRefreshMetadataUpdatesArtworkAndLyricsWithoutReplacingIdenticalContent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	firstImage := resolutionArtwork(t, 16, 16, 10)
	if err := os.WriteFile(filepath.Join(f.root, "good.png"), firstImage, 0600); err != nil {
		t.Fatal(err)
	}
	refreshMetadataScan(t, &f)
	firstCover := selectedRefreshSlot(t, f, "artwork", "")
	firstLyrics := selectedRefreshSlot(t, f, "lyrics", "und")
	firstEmbedded := selectedRefreshSlot(t, f, "lyrics", "en")
	secondImage := resolutionArtwork(t, 32, 32, 30)
	secondLyrics := []byte("[00:02]Updated sidecar lyrics")
	for name, body := range map[string][]byte{"good.png": secondImage, "good.lrc": secondLyrics} {
		if err := os.WriteFile(filepath.Join(f.root, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	results := refreshMetadataScan(t, &f)
	updated := map[string]bool{}
	for _, result := range results {
		if result.SourceTrackID != f.good().ID {
			continue
		}
		for _, action := range result.Actions {
			if action.Status == "applied" {
				updated[action.Field+"/"+action.Language] = true
			}
		}
	}
	if !updated["artwork/"] || !updated["lyrics/und"] || updated["lyrics/en"] {
		t.Fatal("refresh did not distinguish updated and identical metadata", results)
	}
	for key, want := range map[string][]byte{"artwork": secondImage, "lyrics": secondLyrics} {
		language := ""
		old := firstCover
		if key == "lyrics" {
			language, old = "und", firstLyrics
		}
		selected := selectedRefreshSlot(t, f, key, language)
		if selected.ID == old.ID || selected.ManualLocked {
			t.Fatal("refresh did not replace automatic selection", selected)
		}
		_, reader, err := f.content.Open(ctx, "admin", selected.ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(body, want) {
			t.Fatal("refreshed content differs from sidecar", key, err)
		}
	}
	if embedded := selectedRefreshSlot(t, f, "lyrics", "en"); embedded.ID != firstEmbedded.ID {
		t.Fatal("identical embedded lyrics created another binding", embedded)
	}
	before, err := f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range refreshMetadataScan(t, &f) {
		if result.SourceTrackID == f.good().ID {
			for _, action := range result.Actions {
				if action.Status != "skipped" || action.Reason != "unchangedContent" {
					t.Fatal("identical refresh was not reported as unchanged", action)
				}
			}
		}
	}
	after, err := f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("repeated refresh changed selected metadata", after, err)
	}
	missing := selectedRefreshSlot(t, f, "artwork", "")
	object, err := f.content.ContentObject(ctx, "admin", missing.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(f.mediaRoot, filepath.FromSlash(object.RelativePath))); err != nil {
		t.Fatal(err)
	}
	refreshMetadataScan(t, &f)
	repaired := selectedRefreshSlot(t, f, "artwork", "")
	if repaired.ID == missing.ID || repaired.ObjectID == missing.ObjectID {
		t.Fatal("identical refresh retained a missing artwork file", repaired)
	}
	_, file, err := f.content.Open(ctx, "admin", repaired.ObjectID)
	if err != nil {
		t.Fatal("refresh did not repair missing artwork", err)
	}
	file.Close()
	before, err = f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range before.Bindings {
		if binding.Field != "artwork" && binding.Language != "und" {
			continue
		}
		before, err = f.content.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: before.Target, ObjectID: binding.ObjectID, ExpectedRevision: before.Revision}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string][]byte{"good.png": firstImage, "good.lrc": []byte("[00:03]Another sidecar lyric")} {
		if err = os.WriteFile(filepath.Join(f.root, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	locked := 0
	for _, result := range refreshMetadataScan(t, &f) {
		if result.SourceTrackID == f.good().ID {
			for _, action := range result.Actions {
				if action.Reason == "manualLocked" {
					locked++
				}
			}
		}
	}
	after, err = f.content.ContentSelection(ctx, "admin", f.good().ID)
	if err != nil || locked != 2 || !reflect.DeepEqual(before, after) {
		t.Fatal("refresh changed manually selected artwork or lyrics", after, err)
	}
}

func TestRefreshMetadataPreservesCoverWhenSourceHasNoValidPicture(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		name := "missing"
		if corrupt {
			name = "invalid"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			path := filepath.Join(f.root, "good.png")
			if err := os.WriteFile(path, resolutionArtwork(t, 16, 16, 10), 0600); err != nil {
				t.Fatal(err)
			}
			refreshMetadataScan(t, &f)
			before := selectedRefreshSlot(t, f, "artwork", "")
			var err error
			if corrupt {
				err = os.WriteFile(path, []byte("This is not an image"), 0600)
			} else {
				err = os.Remove(path)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range refreshMetadataScan(t, &f) {
				if result.SourceTrackID == f.good().ID {
					for _, action := range result.Actions {
						if action.Field == "artwork" && action.Status == "applied" {
							t.Fatal("invalid or generic directory cover was applied", action)
						}
					}
				}
			}
			if after := selectedRefreshSlot(t, f, "artwork", ""); !reflect.DeepEqual(before, after) {
				t.Fatal("failed refresh discarded previous artwork", after)
			}
		})
	}
}

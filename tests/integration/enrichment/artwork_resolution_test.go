package enrichment_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func resolutionArtwork(t *testing.T, width, height int, red uint8) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	picture.SetRGBA(0, 0, color.RGBA{R: red, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, picture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestImportArtworkPrefersPixelsThenShortEdgeAndStableCandidate(t *testing.T) {
	f := setup(t)
	frames := []testkit.ScraperFrame{
		{ID: "USLT", Body: []byte("\x03eng\x00First synthetic lyric")},
		{ID: "USLT", Body: []byte("\x03eng\x00Different synthetic lyric")},
	}
	for _, picture := range [][]byte{
		resolutionArtwork(t, 2, 2, 10),
		resolutionArtwork(t, 64, 16, 20),
		resolutionArtwork(t, 32, 32, 30),
		resolutionArtwork(t, 32, 32, 40),
	} {
		frames = append(frames, testkit.ScraperFrame{ID: "APIC", Body: append([]byte("\x03image/png\x00\x03\x00"), picture...)})
	}
	if err := os.WriteFile(filepath.Join(f.root, "good.mp3"), testkit.ScraperID3(4, frames...), 0600); err != nil {
		t.Fatal(err)
	}
	options := testkit.ScanPlan("root").Options
	options.ImportArtwork, options.ImportLyrics = true, true
	scan := rescanAutomaticOptions(t, f, options)
	automaticService(t, &f, enrichment.AutomaticPolicy{}, f.runtimes)
	results := automaticFinished(t, f, scan.ID)
	var selected mediastore.ContentObject
	lyricsAmbiguous, artworkSelected := false, false
	for _, result := range results {
		if result.SourceTrackID != f.good().ID {
			continue
		}
		for _, action := range result.Actions {
			if result.Reason == "importLyrics" && action.Language == "en" && action.Reason == "ambiguous" {
				lyricsAmbiguous = true
			}
			if result.Reason != "importArtwork" || action.Field != "artwork" {
				continue
			}
			if action.Status != "applied" || action.CandidateID == nil || result.JobID == nil {
				t.Fatal("multiple artwork candidates were not selected", result)
			}
			job, err := f.service.Get(t.Context(), "admin", *result.JobID)
			if err != nil {
				t.Fatal(err)
			}
			best := ""
			for _, candidate := range job.Targets[0].Candidates {
				if candidate.PreviewObjectID == nil {
					t.Fatal("artwork candidate has no validated object", candidate)
				}
				object, err := f.content.ContentObject(t.Context(), "admin", *candidate.PreviewObjectID)
				if err != nil {
					t.Fatal(err)
				}
				if candidate.CandidateID == *action.CandidateID {
					selected = object
				}
				if object.Width == 32 && object.Height == 32 && (best == "" || candidate.CandidateID < best) {
					best = candidate.CandidateID
				}
			}
			if best == "" || *action.CandidateID != best || selected.Width != 32 || selected.Height != 32 {
				t.Fatal("artwork resolution ordering was not deterministic", action, selected)
			}
			artworkSelected = true
		}
	}
	if !artworkSelected || !lyricsAmbiguous {
		t.Fatal("artwork selection or lyrics ambiguity policy changed", results)
	}
	selection, err := f.content.ContentSelection(t.Context(), "admin", f.good().ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range selection.Bindings {
		if binding.Field == "artwork" {
			found = binding.ObjectID == selected.ID && !binding.ManualLocked
		}
	}
	if !found {
		t.Fatal("highest resolution artwork was not persisted as the selection", selection)
	}
}

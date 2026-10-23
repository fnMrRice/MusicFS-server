package enrichment_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestNCMContainerArtworkIsAutomaticallyPublished(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	plain, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	image := testkit.ScraperPNG()
	container, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{Cover: image, CoverPadding: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "cover.ncm"), container, 0600); err != nil {
		t.Fatal(err)
	}
	runtime, ok := f.lib.Runtime("local")
	if !ok {
		t.Fatal("local source is missing")
	}
	plan := testkit.ScanPlan("root")
	plan.Options.ImportArtwork = true
	if _, _, err := f.store.Enqueue(ctx, "admin", "local", plan, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	scan, err := f.store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var observations []library.Observation
	for _, name := range []string{"good.mp3", "other.mp3", "cover.ncm"} {
		e, err := runtime.Worker.Access.Inspect("root", name)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, library.Observation{Observation: pluginproto.Observation{
			ExternalID: name, RootID: "root", RelativePath: name, ObservedVersion: e.ObservedVersion, SizeBytes: e.SizeBytes, MIME: e.MIME, DisplayName: name,
		}})
	}
	if err := f.store.Publish(ctx, scan, testkit.ScanPublication(scan, observations)); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Finish(ctx, scan, len(observations), nil); err != nil {
		t.Fatal(err)
	}
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	f.service.Close()
	f.service, err = enrichmentworker.New(f.store, f.lib, f.content, f.runtimes, enrichmentworker.Config{NCMDecoder: decoder})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.service.Close)
	if err := f.service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, result := range automaticFinished(t, f, scan.ID) {
		track, err := f.lib.Track(ctx, "admin", result.SourceTrackID)
		if err != nil {
			t.Fatal(err)
		}
		if track.Path != "cover.ncm" || result.Reason != "importArtwork" {
			continue
		}
		found = true
		if result.State != "completed" || len(result.Actions) != 1 || result.Actions[0].Status != "applied" || result.JobID == nil {
			t.Fatal("NCM artwork was not automatically selected", result)
		}
		job, err := f.service.Get(ctx, "admin", *result.JobID)
		if err != nil {
			t.Fatal(err)
		}
		containerCandidate := false
		for _, candidate := range job.Targets[0].Candidates {
			if candidate.Origin == "container" && candidate.Basis == "containerEmbedded" && candidate.Status == "available" {
				containerCandidate = true
			}
		}
		if !containerCandidate {
			t.Fatal("container artwork provenance was lost", job)
		}
		selection, err := f.content.ContentSelection(ctx, "admin", track.ID)
		if err != nil || len(selection.Bindings) != 1 || selection.Bindings[0].ManualLocked {
			t.Fatal("NCM automatic selection was not published", err, selection)
		}
		_, reader, err := f.content.Open(ctx, "admin", selection.Bindings[0].ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(body, image) {
			t.Fatal("published NCM artwork differs from its container", err)
		}
	}
	if !found {
		t.Fatal("NCM import trigger was missing")
	}
	after, err := os.ReadFile(filepath.Join(f.root, "cover.ncm"))
	if err != nil || !bytes.Equal(after, container) {
		t.Fatal("NCM original was changed by automatic artwork import", err)
	}
}

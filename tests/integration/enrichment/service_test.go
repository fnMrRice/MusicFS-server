package enrichment_test

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestMetadataActualWorkerPreviewManualApplyAndReplay(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	track := f.good()
	before, err := os.ReadFile(filepath.Join(f.root, "good.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	j, _, err := f.service.Submit(ctx, "admin", request(track.ID), "actual-scrape-job")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	j = finished(t, f, j.JobID)
	if j.State != "completed" || len(j.Targets) != 1 || len(j.Targets[0].Candidates) != 3 {
		t.Fatalf("unexpected job: %+v", j)
	}
	selection, err := f.store.ContentSelection(ctx, "admin", track.ID)
	if err != nil || selection.Revision != 0 || len(selection.Bindings) != 0 {
		t.Fatal("search applied automatically")
	}
	var lyrics, artwork, sidecar enrichment.PreviewCandidate
	for _, c := range j.Targets[0].Candidates {
		if c.Status != "available" || c.PreviewObjectID == nil {
			t.Fatal("preview missing")
		}
		if c.Field == "artwork" {
			artwork = c
		} else if c.Origin == "embedded" {
			lyrics = c
		} else {
			sidecar = c
		}
	}
	_, file, err := f.content.Open(ctx, "admin", *lyrics.PreviewObjectID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Contains(body, []byte("Embedded fixture lyrics")) || lyrics.Language != "en" || artwork.Language != "" || sidecar.Language != "und" {
		t.Fatal("preview content or language mismatch")
	}
	selection, err = f.service.Apply(ctx, "admin", lyrics.CandidateID, 0, "actual-apply-lyrics")
	if err != nil || selection.Revision != 1 || !selection.Bindings[0].ManualLocked {
		t.Fatal("manual lyrics apply failed", err)
	}
	selection, err = f.service.Apply(ctx, "admin", artwork.CandidateID, 1, "actual-apply-artwork")
	if err != nil || selection.Revision != 2 || len(selection.Bindings) != 2 {
		t.Fatal("manual per-field apply failed", err)
	}
	if err = os.Remove(filepath.Join(f.mediaRoot, "objects", *sidecar.PreviewObjectID)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Apply(ctx, "admin", sidecar.CandidateID, 2, domain.NewID()); err == nil {
		t.Fatal("missing preview selected")
	}
	selection, err = f.store.ContentSelection(ctx, "admin", track.ID)
	if err != nil || selection.Revision != 2 || len(selection.Bindings) != 2 {
		t.Fatal("missing preview lost prior selection")
	}
	if _, err = f.service.Cancel(ctx, "admin", j.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Apply(ctx, "admin", lyrics.CandidateID, 2, domain.NewID()); err == nil {
		t.Fatal("cancelled candidate applied")
	}
	replay, err := f.service.Apply(ctx, "admin", lyrics.CandidateID, 0, "actual-apply-lyrics")
	if err != nil || replay.Revision != 1 {
		t.Fatal("successful operation did not replay", err)
	}
	f.service.Close()
	for i := range f.runtimes {
		f.runtimes[i].Host = nil
	}
	restarted, err := enrichmentworker.New(f.store, f.lib, f.content, f.runtimes, enrichmentworker.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, _, err = restarted.Submit(ctx, "admin", request(track.ID), domain.NewID()); err == nil {
		t.Fatal("disabled scraper accepted new work")
	}
	selection, err = f.store.ContentSelection(ctx, "admin", track.ID)
	if err != nil || selection.Revision != 2 || len(selection.Bindings) != 2 {
		t.Fatal("disabled scraper erased content")
	}
	var document []byte
	if err = f.store.DB.QueryRow("SELECT document FROM enrichment_candidates WHERE id=?", lyrics.CandidateID).Scan(&document); err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(j)
	for _, secret := range []string{"Embedded fixture lyrics", "旁置合成歌词", f.root, "resourceRef", "resourceVersion"} {
		if bytes.Contains(document, []byte(secret)) || bytes.Contains(public, []byte(secret)) {
			t.Fatal("private content reached job index")
		}
	}
	after, err := os.ReadFile(filepath.Join(f.root, "good.mp3"))
	if err != nil || sha512.Sum512(after) != sha512.Sum512(before) {
		t.Fatal("original changed")
	}
}

func TestMetadataBatchPartialFailureLanguagesAndUnsupportedCorrections(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	track := f.good()
	r := request(track.ID)
	r.Query = &enrichment.Query{Title: "Correction"}
	if _, _, err := f.service.Submit(ctx, "admin", r, domain.NewID()); err == nil {
		t.Fatal("unsupported correction silently accepted")
	}
	r = request(track.ID)
	r.ExcludeInstanceIDs = []string{"scraper"}
	if _, _, err := f.service.Submit(ctx, "admin", r, domain.NewID()); err == nil {
		t.Fatal("conflicting instance policy accepted")
	}
	if len(f.service.Instances(ctx, "another")) != 0 {
		t.Fatal("private scraper instance exposed")
	}
	r = request(f.tracks[0].ID, f.tracks[1].ID)
	j, _, err := f.service.Submit(ctx, "admin", r, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(f.root, "other.mp3")); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	j = finished(t, f, j.JobID)
	if j.State != "completed" || j.Targets[0].State == j.Targets[1].State {
		t.Fatal("one source failure discarded batch", j)
	}
	r = request(track.ID)
	r.Fields = []string{"lyrics"}
	r.Languages = []string{"zh"}
	languageJob, _, err := f.service.Submit(ctx, "admin", r, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	languageJob = finished(t, f, languageJob.JobID)
	if len(languageJob.Targets[0].Candidates) != 0 || len(languageJob.Targets[0].Issues) == 0 {
		t.Fatal("unknown/English lyrics were claimed as Chinese")
	}
	r.Languages = []string{"en"}
	languageJob, _, err = f.service.Submit(ctx, "admin", r, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	languageJob = finished(t, f, languageJob.JobID)
	if len(languageJob.Targets[0].Candidates) != 1 || languageJob.Targets[0].Candidates[0].Language != "en" {
		t.Fatal("declared ID3 language alias not matched")
	}
}

func TestMetadataBusyScraperWaitsWithinTargetDeadline(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	track := f.good()
	occupied, err := f.runtimes[0].Host.Open(ctx, pluginproto.Context{UserID: "admin", InstanceID: "scraper", TaskID: domain.NewID(), TimeoutMs: 30000}, enrichment.Target{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	j, _, err := f.service.Submit(ctx, "admin", request(track.ID), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err = occupied.Close(); err != nil {
		t.Fatal(err)
	}
	j = finished(t, f, j.JobID)
	if j.State != "completed" || len(j.Targets[0].Candidates) != 3 {
		t.Fatal("temporary scraper contention lost the search", j)
	}
}

package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestWritebackCommitAtomicallyRebindsContentAndInvalidatesEvidence(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	linked, err := f.s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{f.track.ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := f.s.BeginEvidence(ctx, "admin", hashBinding(f.track.Asset))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
		t.Fatal(err)
	}
	fpJob := fpClaim(t, f.s, fpTarget(f.track))
	publication, raw := fpPublication(t, fpJob)
	fpAdvance(t, f.s, fpFiles(t, t.TempDir()), publication, raw, "referenced")
	key := "writeback-commit-request"
	j, _, err := f.s.QueueWriteback(ctx, "admin", f.request, f.target, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 2); err != nil {
		t.Fatal(err)
	}
	r, o := f.result(j, true)
	o.Artists = []string{" 甲 /乙 ", "甲", " /丙/ "}
	wrong := o
	wrong.RelativePath = "other.mp3"
	if _, err = f.s.CommitWriteback(ctx, "admin", j.ID, r, wrong); err == nil {
		t.Fatal("different installed path committed")
	}
	unchanged, err := f.s.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || unchanged.AssetID != f.track.AssetID {
		t.Fatal("failed commit changed source", err)
	}
	before, err := f.s.ContentSelection(ctx, "admin", f.track.ID)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.s.CommitWriteback(ctx, "admin", j.ID, r, o)
	if err != nil || !out.Changed || out.AssetID == f.track.AssetID || out.PreviousAssetID != f.track.AssetID || out.FileSHA512 != r.New.SHA512 {
		t.Fatal(out, err)
	}
	fresh, err := f.s.FindTrack(ctx, "admin", f.track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh.Artists, []string{"甲", "乙", "丙"}) {
		t.Fatalf("writeback artists were not normalized: %v", fresh.Artists)
	}
	if fresh.ID != f.track.ID || fresh.ExternalID != f.track.ExternalID || fresh.AssetID != out.AssetID || fresh.Version != r.New.Version || fresh.Size != r.New.Size || fresh.Asset.FileSHA512 == nil || *fresh.Asset.FileSHA512 != r.New.SHA512 || fresh.Asset.HashStatus != "verified" {
		t.Fatalf("new source projection invalid: %+v", fresh)
	}
	old, err := f.s.Asset(ctx, "admin", f.track.AssetID)
	if err != nil || old.Current {
		t.Fatal("old asset still current", err)
	}
	var current int
	if err = f.s.DB.QueryRow("SELECT current FROM fingerprint_indexes WHERE asset_id=?", f.track.AssetID).Scan(&current); err != nil || current != 0 {
		t.Fatal("old fingerprint stayed current", err)
	}
	history, err := f.s.EvidenceHistory(ctx, "admin", f.track.AssetID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range history {
		if e.Evidence.InvalidatedAt == nil || e.Evidence.Status != "invalidated" {
			t.Fatal("old hash/evidence still active")
		}
	}
	after, err := f.s.ContentSelection(ctx, "admin", f.track.ID)
	if err != nil || after.Revision != before.Revision+1 || after.Target.AssetID != out.AssetID || len(after.Bindings) != 2 {
		t.Fatal(after, err)
	}
	for i, b := range after.Bindings {
		if !b.Current || b.ID == before.Bindings[i].ID || b.ObjectID != before.Bindings[i].ObjectID || b.ManualLocked != before.Bindings[i].ManualLocked || b.Target.AssetID != out.AssetID {
			t.Fatal("binding or manual lock not preserved", b)
		}
	}
	object, err := f.s.ContentObject(ctx, "admin", f.english.ID)
	if err != nil || object.Metadata.Target.AssetID != f.track.AssetID {
		t.Fatal("original content provenance overwritten", err)
	}
	var count int
	if err = f.s.DB.QueryRow("SELECT COUNT(*) FROM media_content_bindings WHERE source_id=?", f.track.ID).Scan(&count); err != nil || count != 4 {
		t.Fatal("binding history lost", count, err)
	}
	link, err := f.s.GetLink(ctx, "admin", f.track.ID)
	if err != nil || link.RecordingID == nil || *link.RecordingID != *linked.RecordingID {
		t.Fatal("recording membership changed", err)
	}
	again, err := f.s.CommitWriteback(ctx, "admin", j.ID, r, o)
	if err != nil || !reflect.DeepEqual(out, again) {
		t.Fatal("commit did not replay", err)
	}
	done, err := f.s.CheckWriteback(ctx, "admin", j.ID, f.target, 2)
	if err != nil || !done {
		t.Fatal("committed old binding was not recognized", err)
	}
	replayed, ok, err := f.s.ReplayWriteback(ctx, "admin", f.request, key)
	if err != nil || !ok || replayed.Status != "succeeded" || replayed.Result == nil {
		t.Fatal("request replay required the old source", err)
	}
	mutated := r
	mutated.New.Size++
	if _, err = f.s.CommitWriteback(ctx, "admin", j.ID, mutated, o); err == nil {
		t.Fatal("different result replayed")
	}
	// Rebound content can be explicitly written again without rewriting its
	// immutable object provenance to pretend it originated from the new asset.
	nextRequest := f.request
	nextRequest.AssetID = out.AssetID
	nextRequest.AssetVersion = out.AssetVersion
	nextRequest.ExpectedRevision = after.Revision
	nextTarget := f.target
	nextTarget.AssetID = out.AssetID
	nextTarget.AssetVersion = out.AssetVersion
	nextTarget.FileIdentity = r.New.Identity
	if _, _, err = f.s.QueueWriteback(ctx, "admin", nextRequest, nextTarget, domain.NewID()); err != nil {
		t.Fatal("rebound selected content rejected", err)
	}
}

func TestWritebackNoChangePreservesAssetAndSelection(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	j := f.queue(t)
	if err := f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 2); err != nil {
		t.Fatal(err)
	}
	r, o := f.result(j, false)
	out, err := f.s.CommitWriteback(ctx, "admin", j.ID, r, o)
	if err != nil || out.Changed || out.AssetID != f.track.AssetID {
		t.Fatal(out, err)
	}
	selection, err := f.s.ContentSelection(ctx, "admin", f.track.ID)
	if err != nil || selection.Revision != 2 {
		t.Fatal("no-op advanced selection", err)
	}
	if err = f.s.RecoverWritebackQueue(ctx, map[string]bool{j.ID: true}); err != nil {
		t.Fatal(err)
	}
	done, err := f.s.GetWriteback(ctx, "admin", j.ID)
	if err != nil || done.Status != "succeeded" {
		t.Fatal("recovery reset committed job", err)
	}
}

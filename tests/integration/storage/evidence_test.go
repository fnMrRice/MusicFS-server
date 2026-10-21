package storage_test

import (
	"context"
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

func evidenceStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "evidence.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func publishEvidenceTracks(t *testing.T, s *sqlite.Store, items ...library.Observation) []library.Track {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("music"), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, j, testkit.ScanPublication(j, items)); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, j, len(items), nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListTracks(ctx, "admin", "local", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func evidenceObservation(id, version string) library.Observation {
	return library.Observation{Observation: pluginproto.Observation{ExternalID: id, RootID: "music", RelativePath: id + ".mp3", ObservedVersion: version, DisplayName: id, MIME: "audio/mpeg", SizeBytes: 1000}}
}

func hashBinding(a *identification.Asset) identification.Binding {
	return identification.Binding{SourceTrackID: a.SourceTrackID, AssetID: a.ID, AssetVersion: a.Version, Spec: identification.Spec{Kind: "fileSha512", Origin: "probe", Algorithm: "sha512", AlgorithmVersion: "sha512-v1", ConfigRevision: "full-file-v1", Range: identification.Range{Unit: "bytes", End: a.FileSizeBytes}}}
}

func hashResult() identification.Result {
	digest := strings.Repeat("a", 128)
	now := time.Now().In(time.FixedZone("test-zone", 8*3600))
	return identification.Result{Status: "verified", BitrateMode: "unknown", SHA512: &digest, ComputedAt: &now}
}

func TestAssetEvidenceNullsProvenanceAndManualPriority(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	unknown := evidenceObservation("unknown", "v1")
	unknown.SizeBytes = 0
	first := publishEvidenceTracks(t, s, unknown)[0]
	if first.Asset == nil || first.Asset.HashStatus != "unknown" || first.Asset.HashComputedAt != nil || first.Asset.BitrateBps != nil || first.Asset.BitrateMode != "unknown" || first.Asset.BitrateOrigin != nil || first.Asset.FileSizeBytes == nil || *first.Asset.FileSizeBytes != 0 {
		t.Fatalf("unknown and known zero were conflated: %+v", first.Asset)
	}
	declared := int64(999000)
	measured := int64(187643)
	mode, providerHash := "vbr", strings.Repeat("b", 128)
	o := evidenceObservation("vbr", "v1")
	o.BitrateBps, o.BitrateMode, o.ProviderSHA512 = &declared, &mode, &providerHash
	now := time.Now().UTC()
	probe := identification.Evidence{Spec: identification.Spec{Kind: "bitrate", Origin: "probe", Algorithm: "ffprobe", AlgorithmVersion: "test-binary-identity", ConfigRevision: "first-audio-v1", Range: identification.Range{Unit: "milliseconds"}}, Result: identification.Result{Status: "reported", BitrateBps: &measured, BitrateMode: "unknown", ComputedAt: &now}}
	o.Evidence = []identification.Evidence{probe}
	o.ProbeStatus = "succeeded"
	tracks := publishEvidenceTracks(t, s, unknown, o)
	a := tracks[1].Asset
	if *a.BitrateBps != measured || *a.BitrateOrigin != "probe" || a.BitrateMode != "unknown" || *a.ProviderSHA512 != providerHash || a.FileSHA512 != nil || a.HashStatus != "unknown" || len(a.Evidence) != 3 {
		t.Fatalf("probe and provider claims were conflated: %+v", a)
	}
	if *a.Evidence[0].BitrateBps != declared || a.Evidence[0].BitrateMode != "vbr" {
		t.Fatal("original incorrect provider declaration was lost")
	}
	binding := hashBinding(a)
	binding.Kind, binding.Origin, binding.Algorithm = "bitrate", "manual", "manual-entry"
	ticket, err := s.BeginEvidence(ctx, "admin", binding)
	if err != nil {
		t.Fatal(err)
	}
	manual := int64(190000)
	if err := s.PublishEvidence(ctx, "admin", ticket, identification.Result{Status: "reported", BitrateBps: &manual, BitrateMode: "vbr", ComputedAt: &now}); err != nil {
		t.Fatal(err)
	}
	tracks = publishEvidenceTracks(t, s, unknown, o)
	after := tracks[1].Asset
	if after.ID != a.ID || *after.BitrateBps != manual || *after.BitrateOrigin != "manual" || after.BitrateMode != "vbr" {
		t.Fatal("rescan overrode manual evidence")
	}
}

func TestEvidencePublicationSnapshotAndSameAssetRescan(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	one, two := evidenceObservation("a", "v1"), evidenceObservation("b", "v1")
	tracks := publishEvidenceTracks(t, s, one, two)
	page, err := s.ListTracks(ctx, "admin", "local", "", 1)
	if err != nil || page.NextCursor == nil {
		t.Fatal("missing fixed snapshot", err)
	}
	target := tracks[1].Asset
	ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(target))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Asset(ctx, "admin", target.ID)
	if err != nil || pending.HashStatus != "pending" || pending.FileSHA512 != nil {
		t.Fatal("persisted task not pending", err)
	}
	result := hashResult()
	if err := s.PublishEvidence(ctx, "admin", ticket, result); err != nil {
		t.Fatal(err)
	}
	got, err := s.Asset(ctx, "admin", target.ID)
	if err != nil || got.HashStatus != "verified" || got.FileSHA512 == nil || *got.FileSHA512 != *result.SHA512 || got.HashComputedAt.Location() != time.UTC {
		t.Fatalf("verified projection missing or not UTC: %+v %v", got, err)
	}
	var size int
	if err := s.DB.QueryRow("SELECT length(file_sha512) FROM source_assets WHERE id=?", target.ID).Scan(&size); err != nil || size != 64 {
		t.Fatal("digest is not stored as 64 bytes", size, err)
	}
	old, err := s.ListTracks(ctx, "admin", "local", *page.NextCursor, 1)
	if err != nil || old.LibraryRevision != page.LibraryRevision || old.Items[0].Asset.HashStatus != "unknown" {
		t.Fatal("evidence publication mutated an existing list snapshot", err)
	}
	after := publishEvidenceTracks(t, s, one, two)[1]
	if after.ID != tracks[1].ID || after.AssetID != target.ID || after.Asset.HashStatus != "verified" || *after.Asset.FileSHA512 != *result.SHA512 {
		t.Fatal("unchanged rescan reset verified evidence")
	}
	if err := s.PublishEvidence(ctx, "admin", ticket, result); err == nil {
		t.Fatal("completed ticket could publish twice")
	}
}

func TestEvidenceAssetChangeInvalidatesLateTasksAndKeepsAudit(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	o := evidenceObservation("same-track", "original")
	first := publishEvidenceTracks(t, s, o)[0]
	oldTicket, err := s.BeginEvidence(ctx, "admin", hashBinding(first.Asset))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishEvidence(ctx, "admin", oldTicket, hashResult()); err != nil {
		t.Fatal(err)
	}
	late, err := s.BeginEvidence(ctx, "admin", hashBinding(first.Asset))
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"tag-only-edit", "different-quality"} {
		o.ObservedVersion = version
		current := publishEvidenceTracks(t, s, o)[0]
		if current.ID != first.ID || current.AssetID == first.AssetID || current.Asset.HashStatus != "unknown" || current.Asset.FileSHA512 != nil {
			t.Fatal("asset change altered stable track identity or inherited evidence")
		}
		if err := s.PublishEvidence(ctx, "admin", late, hashResult()); err == nil {
			t.Fatal("old asset task updated replacement bytes")
		}
		late, err = s.BeginEvidence(ctx, "admin", hashBinding(current.Asset))
		if err != nil {
			t.Fatal(err)
		}
	}
	oldAsset, err := s.Asset(ctx, "admin", first.AssetID)
	if err != nil || oldAsset.Current || oldAsset.HashStatus != "invalidated" || oldAsset.FileSHA512 != nil || len(oldAsset.Evidence) != 0 {
		t.Fatal("old asset remained current evidence", err)
	}
	history, err := s.EvidenceHistory(ctx, "admin", first.AssetID, 0, 1)
	if err != nil || len(history) != 1 || history[0].Evidence.Status != "invalidated" {
		t.Fatal("audit page missing", err)
	}
	older, err := s.EvidenceHistory(ctx, "admin", first.AssetID, history[0].Sequence, 10)
	if err != nil || len(older) != 1 || older[0].Evidence.SHA512 == nil || older[0].Evidence.ComputedAt == nil || older[0].Evidence.InvalidatedAt == nil {
		t.Fatal("verified historical evidence was discarded", err)
	}
	if _, err := s.Asset(ctx, "another-user", first.AssetID); err == nil {
		t.Fatal("asset history leaked to another user")
	}
	if _, err := s.EvidenceHistory(ctx, "another-user", first.AssetID, 0, 10); err == nil {
		t.Fatal("evidence audit leaked to another user")
	}
	if err := s.PublishEvidence(ctx, "another-user", late, hashResult()); err == nil {
		t.Fatal("cross-user task publication accepted")
	}
	missing := publishEvidenceTracks(t, s)[0]
	if missing.Availability != "missing" || missing.Asset.Current {
		t.Fatal("missing source kept current evidence")
	}
	if err := s.PublishEvidence(ctx, "admin", late, hashResult()); err == nil {
		t.Fatal("missing source accepted a late result")
	}
}

func TestEvidenceGenerationFailuresAndAtomicRollback(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	o := evidenceObservation("track", "v1")
	a := publishEvidenceTracks(t, s, o)[0].Asset
	first, err := s.BeginEvidence(ctx, "admin", hashBinding(a))
	if err != nil {
		t.Fatal(err)
	}
	changed := hashBinding(a)
	changed.AlgorithmVersion, changed.ConfigRevision = "sha512-v2", "new-config"
	latest, err := s.BeginEvidence(ctx, "admin", changed)
	if err != nil || latest.Generation <= first.Generation {
		t.Fatal("task generation did not advance", err)
	}
	if err := s.PublishEvidence(ctx, "admin", first, hashResult()); err == nil {
		t.Fatal("superseded algorithm/config task accepted")
	}
	forged := latest
	forged.Range.End = nil
	if err := s.PublishEvidence(ctx, "admin", forged, hashResult()); err == nil {
		t.Fatal("task range was not bound")
	}
	if err := s.CancelEvidence(ctx, "admin", latest); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishEvidence(ctx, "admin", latest, hashResult()); err == nil {
		t.Fatal("cancelled task accepted")
	}
	failed, _ := s.Asset(ctx, "admin", a.ID)
	if failed.HashStatus != "failed" || failed.FileSHA512 != nil || *failed.Evidence[0].FailureCode != "CANCELED" {
		t.Fatal("cancelled task fabricated a hash")
	}
	latest, err = s.BeginEvidence(ctx, "admin", hashBinding(a))
	if err != nil {
		t.Fatal(err)
	}
	code := "UNSUPPORTED"
	if err := s.PublishEvidence(ctx, "admin", latest, identification.Result{Status: "unavailable", BitrateMode: "unknown", FailureCode: &code}); err != nil {
		t.Fatal(err)
	}
	unavailable, _ := s.Asset(ctx, "admin", a.ID)
	if unavailable.HashStatus != "unavailable" || unavailable.HashComputedAt != nil {
		t.Fatal("unavailable hash confused with pending/computed")
	}
	// A failure after invalidation must roll back both the task and the public view.
	if _, err := s.DB.Exec("CREATE TRIGGER reject_evidence_insert BEFORE INSERT ON asset_evidence BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginEvidence(ctx, "admin", hashBinding(a)); err == nil {
		t.Fatal("injected storage failure was ignored")
	}
	after, _ := s.Asset(ctx, "admin", a.ID)
	beforeJSON, _ := json.Marshal(unavailable)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("failed task creation partially changed evidence")
	}
	if _, err := s.DB.Exec("DROP TRIGGER reject_evidence_insert"); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.BeginEvidence(canceled, "admin", hashBinding(a)); err == nil {
		t.Fatal("cancelled begin created a task")
	}
}

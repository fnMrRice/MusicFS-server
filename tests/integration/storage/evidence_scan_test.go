package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/identification"
)

func TestSameAssetScanWithdrawsClaimsAndReconcilesSuccessfulProbe(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	o := evidenceObservation("same", "v1")
	rate := int64(320000)
	mode, digest := "cbr", strings.Repeat("b", 128)
	o.BitrateBps, o.BitrateMode, o.ProviderSHA512 = &rate, &mode, &digest
	now := time.Now().UTC()
	probe := identification.Evidence{Spec: identification.Spec{Kind: "bitrate", Origin: "probe", Algorithm: "ffprobe", AlgorithmVersion: "1", ConfigRevision: "audio-v1", Range: identification.Range{Unit: "milliseconds"}}, Result: identification.Result{Status: "reported", BitrateBps: &rate, BitrateMode: "unknown", ComputedAt: &now}}
	o.ProbeStatus, o.Evidence = "succeeded", []identification.Evidence{probe}
	first := publishEvidenceTracks(t, s, o)[0]
	ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(first.Asset))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
		t.Fatal(err)
	}
	// Provider nulls revoke earlier claims. A successful estimate replaces this
	// probe's earlier measured value rather than losing to its old priority.
	o.BitrateBps, o.BitrateMode, o.ProviderSHA512 = nil, nil, nil
	estimated := int64(150000)
	probe.Origin, probe.BitrateBps = "estimate", &estimated
	o.Evidence = []identification.Evidence{probe}
	a := publishEvidenceTracks(t, s, o)[0].Asset
	if a.ID != first.AssetID || *a.BitrateBps != estimated || *a.BitrateOrigin != "estimate" || a.ProviderSHA512 != nil || a.HashStatus != "verified" {
		t.Fatalf("rescanned claims were stale or verified hash was reset: %+v", a)
	}
	for _, e := range a.Evidence {
		if e.Origin == "provider" || e.Kind == "bitrate" && e.Origin == "probe" {
			t.Fatal("withdrawn evidence remained active")
		}
	}
	// Failure is not successful unknown data: the same-byte prior estimate stays.
	o.ProbeStatus, o.Evidence = "failed", nil
	a = publishEvidenceTracks(t, s, o)[0].Asset
	if a.BitrateBps == nil || *a.BitrateBps != estimated || *a.BitrateOrigin != "estimate" {
		t.Fatal("failed probe invented an authoritative empty result")
	}
	o.ProbeStatus = "succeeded"
	a = publishEvidenceTracks(t, s, o)[0].Asset
	if a.BitrateBps != nil || a.BitrateMode != "unknown" || a.BitrateOrigin != nil || a.HashStatus != "verified" {
		t.Fatal("successful probe with no bitrate retained stale evidence")
	}
	history, err := s.EvidenceHistory(ctx, "admin", a.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	invalidated := 0
	for _, item := range history {
		if item.Evidence.Status == "invalidated" {
			invalidated++
		}
	}
	if invalidated != 4 {
		t.Fatalf("withdrawn provider/probe history not retained: %d", invalidated)
	}
}

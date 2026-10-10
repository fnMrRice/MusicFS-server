package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

// These assertions exercise the published HTTP view after trusted repository
// writes; they do not claim that the future background hasher read a file.
func TestLibraryEvidenceHTTPProjection(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	publish := func(version, key string) {
		t.Helper()
		if _, _, err := store.Enqueue(ctx, "admin", "mine", testkit.ScanPlan("mine"), key); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item := library.Observation{Observation: pluginproto.Observation{
			ExternalID: "one", RootID: "mine", RelativePath: "private/path/vbr.mp3", ObservedVersion: version, DisplayName: "vbr.mp3",
			MIME: "audio/mpeg", SizeBytes: 33432, BitrateBps: evidenceValue(int64(44576)), BitrateMode: evidenceValue("vbr"), ProviderSHA512: evidenceValue(strings.Repeat("a", 128))}, Artists: []string{}}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{item})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	read := func() library.Track {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/source-tracks", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "SourceTracks", w.Body.Bytes())
		if strings.Contains(w.Body.String(), "private/path") {
			t.Fatal("evidence response exposed a source path")
		}
		var page library.Page
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
			t.Fatal("invalid evidence page", err)
		}
		track := page.Items[0]
		if track.Asset == nil || track.AssetID != track.Asset.ID || track.ID != track.Asset.SourceTrackID || !track.Asset.Current {
			t.Fatal("current asset is not bound to the published source track")
		}
		if track.BitrateBps == nil || track.Asset.BitrateBps == nil || *track.BitrateBps != *track.Asset.BitrateBps {
			t.Fatal("top-level bitrate drifted from the current asset")
		}
		for _, evidence := range track.Asset.Evidence {
			if evidence.AssetID != track.AssetID || evidence.AssetVersion != track.Asset.Version || evidence.InvalidatedAt != nil {
				t.Fatal("current response included stale or foreign evidence")
			}
		}
		return track
	}
	publish("version-1", "evidence-scan-first")
	track := read()
	asset := track.Asset
	if asset.HashStatus != "unknown" || asset.FileSHA512 != nil || asset.HashComputedAt != nil || asset.ProviderSHA512 == nil ||
		asset.BitrateMode != "vbr" || asset.BitrateOrigin == nil || *asset.BitrateOrigin != "provider" {
		t.Fatal("provider claims were lost or presented as measured evidence")
	}
	binding := identification.Binding{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: asset.Version,
		Spec: identification.Spec{Kind: "fileSha512", Origin: "probe", Algorithm: "sha512", AlgorithmVersion: "1", ConfigRevision: "1",
			Range: identification.Range{Unit: "bytes", End: asset.FileSizeBytes}}}
	for _, status := range []string{"failed", "unavailable", "verified"} {
		ticket, err := store.BeginEvidence(ctx, "admin", binding)
		if err != nil {
			t.Fatal(err)
		}
		pending := read().Asset
		if pending.HashStatus != "pending" || pending.FileSHA512 != nil || pending.HashComputedAt != nil {
			t.Fatal("pending work was confused with an unavailable or verified digest")
		}
		result := identification.Result{Status: status, BitrateMode: "unknown"}
		if status == "verified" {
			result.SHA512, result.ComputedAt = evidenceValue(strings.Repeat("b", 128)), evidenceValue(time.Now().UTC())
		} else {
			result.FailureCode = evidenceValue("SOURCE_OFFLINE")
		}
		if err := store.PublishEvidence(ctx, "admin", ticket, result); err != nil {
			t.Fatal(err)
		}
		current := read().Asset
		if current.HashStatus != status || current.ProviderSHA512 == nil || *current.ProviderSHA512 != strings.Repeat("a", 128) {
			t.Fatal("hash status or provider claim changed unexpectedly")
		}
		if status == "verified" {
			if current.FileSHA512 == nil || *current.FileSHA512 != strings.Repeat("b", 128) || current.HashComputedAt == nil {
				t.Fatal("verified digest did not reach the HTTP projection")
			}
			if current.HashConflict == nil || current.HashConflict.Reason != "PROVIDER_SHA512_MISMATCH" || current.HashConflict.ResolvedAt != nil || current.HashConflict.VerifiedEvidenceID != ticket.ID || current.HashConflict.ProviderEvidenceID == "" {
				t.Fatal("mismatched provider claim did not expose its current conflict evidence")
			}
		} else if current.FileSHA512 != nil || current.HashComputedAt != nil {
			t.Fatal("failed or unavailable digest must remain null")
		}
	}
	publish("version-2", "evidence-scan-replaced")
	replaced := read()
	if replaced.ID != track.ID || replaced.AssetID == track.AssetID || replaced.Asset.HashStatus != "unknown" || replaced.Asset.FileSHA512 != nil || replaced.Asset.HashConflict != nil {
		t.Fatal("asset replacement changed the source identity or retained old proof")
	}
}

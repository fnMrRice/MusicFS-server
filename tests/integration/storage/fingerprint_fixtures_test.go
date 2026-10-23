package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

// Synthetic extractor identities and arrays test storage boundaries only. Real
// audio/tool behavior belongs to the fingerprint worker integration suite.
func fpIdentity(label string) fingerprint.Identity {
	id := fingerprint.Identity{Algorithm: "chromaprint", AlgorithmVersion: "1.6.1", AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision, Analyzer: fingerprint.ToolIdentity{Version: "fpcalc version 1.6.1 (test)", SHA256: strings.Repeat("a", 64)}, Decoder: fingerprint.ToolIdentity{Version: "test-" + label, SHA256: strings.Repeat("b", 64)}}
	raw, _ := json.Marshal(id)
	sum := sha256.Sum256(raw)
	id.ConfigRevision = "sha256-" + hex.EncodeToString(sum[:])
	return id
}

func fpTarget(track library.Track) identification.FingerprintTarget {
	return identification.FingerprintTarget{HashTarget: identification.HashTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, FileSizeBytes: *track.Asset.FileSizeBytes}, FingerprintRequest: identification.FingerprintRequest{StartMs: 13, DurationMs: 30000, ExtractorIdentity: fpIdentity("v1")}}
}

func fpClaim(t *testing.T, s *sqlite.Store, target identification.FingerprintTarget) identification.FingerprintJob {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.QueueFingerprint(ctx, "admin", target, domain.NewID(), false); err != nil {
		t.Fatal(err)
	}
	j, err := s.ClaimFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func fpPublication(t *testing.T, j identification.FingerprintJob) (mediastore.Publication, []byte) {
	t.Helper()
	start, count, err := (fingerprint.Request{TaskID: j.ID, AssetID: j.AssetID, StartMs: j.StartMs, DurationMs: j.DurationMs}).Samples()
	if err != nil {
		t.Fatal(err)
	}
	values := make([]uint32, 20)
	for i := range values {
		values[i] = uint32(i * 173)
	}
	detail := fingerprint.Detail{SchemaVersion: 1, Identity: j.ExtractorIdentity, RequestedStartMs: j.StartMs, RequestedDurationMs: j.DurationMs, PCM: audio.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, StartSample: start, SampleCount: count, DecoderVersion: j.ExtractorIdentity.Decoder.Version, DecoderSHA256: j.ExtractorIdentity.Decoder.SHA256}, Quality: fingerprint.Assess(values, count), Values: values}
	raw, err := detail.Encode()
	if err != nil {
		t.Fatal(err)
	}
	summary := identification.FingerprintSummary{ExtractorIdentity: detail.Identity, ActualStartSample: start, SampleCount: count, ValueCount: len(values), Quality: detail.Quality, ComputedAt: time.Now().In(time.FixedZone("test", 8*3600))}
	metadata, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.NewID()
	now := time.Now().UTC()
	p := mediastore.Publication{ID: domain.NewID(), Owner: j.Owner, Kind: "fingerprint", Status: "prepared", Object: mediastore.Object{ID: id, Kind: "fingerprint", MIME: "application/json", RelativePath: "objects/" + id}, Target: mediastore.Target{Purpose: "fingerprint", ID: j.ID, Generation: j.Generation, Metadata: metadata}, CreatedAt: now, UpdatedAt: now}
	return p, raw
}

func fpValidator(in io.ReadSeeker) error {
	var d fingerprint.Detail
	decoder := json.NewDecoder(in)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return io.ErrUnexpectedEOF
	}
	_, err := d.Encode()
	return err
}

func fpFiles(t *testing.T, path string) *mediastore.Files {
	t.Helper()
	f, err := mediastore.OpenFiles(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}

func fpAdvance(t *testing.T, s *sqlite.Store, f *mediastore.Files, p mediastore.Publication, raw []byte, stage string) mediastore.Publication {
	t.Helper()
	ctx := context.Background()
	if err := s.BeginPublication(ctx, p); err != nil {
		t.Fatal(err)
	}
	if stage == "prepared" {
		return p
	}
	o, err := f.Write(ctx, p.Object.ID, p.Kind, p.Object.MIME, bytes.NewReader(raw), fingerprint.MaxDetailBytes, fpValidator)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.WrittenPublication(ctx, p.Owner, p.ID, o); err != nil {
		t.Fatal(err)
	}
	p.Object, p.Status = o, "written"
	if stage == "written" {
		return p
	}
	if err = f.Publish(o); err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationStage(ctx, p.Owner, p.ID, "written", "published"); err != nil {
		t.Fatal(err)
	}
	p.Status = "published"
	if stage == "published" {
		return p
	}
	if err = s.ReferencePublication(ctx, p.Owner, p.ID); err != nil {
		t.Fatal(err)
	}
	p.Status = "referenced"
	return p
}

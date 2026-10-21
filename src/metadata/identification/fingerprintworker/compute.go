package fingerprintworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func failureCode(err error) string {
	var known *domain.Error
	if errors.As(err, &known) {
		return known.Code
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TEMPORARY_FAILURE"
	}
	return pluginproto.ErrorCode(err)
}

func (s *Service) execute(parent context.Context, j identification.FingerprintJob) {
	ctx, cancel := context.WithTimeout(parent, s.config.Timeout)
	s.mu.Lock()
	s.running[j.ID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, j.ID); s.mu.Unlock() }()
	current, err := s.repo.GetFingerprintJob(ctx, j.Owner, j.ID)
	if err != nil || current.Status != "running" || current.Generation != j.Generation {
		return
	}
	err = s.extractAndPublish(ctx, j)
	if err == nil || parent.Err() != nil {
		return
	}
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if failed := s.repo.FailFingerprint(finish, j, failureCode(err)); failed != nil {
		latest, getErr := s.repo.GetFingerprintJob(finish, j.Owner, j.ID)
		if getErr != nil || latest.Status == "running" && latest.Generation == j.Generation {
			slog.Error("Fingerprint task requires recovery", "jobId", j.ID, "errorCode", failureCode(failed))
		}
	}
}

func (s *Service) extractAndPublish(ctx context.Context, j identification.FingerprintJob) error {
	extractor, decoder, gate, epoch := s.runtime()
	if extractor == nil {
		return domain.Fail("PLUGIN_DISABLED", "Fingerprint audio dependency is unavailable")
	}
	lease, err := gate.AcquireEpoch(ctx, epoch)
	if err != nil {
		return err
	}
	defer lease.Close()
	if lease != nil {
		ctx = lease.Context()
	}
	if j.ExtractorIdentity != extractor.Identity() {
		return domain.Fail("RESOURCE_CHANGED", "Fingerprint extractor configuration changed")
	}
	in, err := s.source.OpenStableHash(ctx, j.Owner, j.ID, j.HashTarget)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = in.Validate(); err != nil {
		return err
	}
	var input io.ReaderAt = in
	size := j.FileSizeBytes
	var header [8]byte
	n, readErr := in.ReadAt(header[:], 0)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if n == len(header) && string(header[:]) == "CTENFDAM" {
		if decoder == nil {
			return domain.Fail("UNSUPPORTED", "NCM decryption plugin is unavailable")
		}
		decoded, err := decoder.Open(ctx, j.ID, j.AssetID, in, size)
		if err != nil {
			return err
		}
		defer decoded.Close()
		input, size = decoded, decoded.Size()
	}
	detail, err := extractor.Extract(ctx, fingerprint.Request{TaskID: j.ID, AssetID: j.AssetID, StartMs: j.StartMs, DurationMs: j.DurationMs}, input, size)
	if err != nil {
		return err
	}
	if err = in.Validate(); err != nil {
		return err
	}
	if detail.Identity != j.ExtractorIdentity || detail.RequestedStartMs != j.StartMs || detail.RequestedDurationMs != j.DurationMs {
		return domain.Fail("PROTOCOL_MISMATCH", "Fingerprint result does not match its task")
	}
	raw, err := detail.Encode()
	if err != nil {
		return err
	}
	summary := identification.FingerprintSummary{ExtractorIdentity: detail.Identity, ActualStartSample: detail.PCM.StartSample, SampleCount: detail.PCM.SampleCount, ValueCount: len(detail.Values), Quality: detail.Quality, ComputedAt: time.Now().UTC()}
	metadata, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	target := mediastore.Target{Purpose: "fingerprint", ID: j.ID, Generation: j.Generation, Metadata: metadata}
	_, err = s.media.Publish(ctx, j.Owner, "fingerprint", "application/json", target, bytes.NewReader(raw), fingerprint.MaxDetailBytes, func(input io.ReadSeeker) error {
		copy, err := io.ReadAll(io.LimitReader(input, fingerprint.MaxDetailBytes+1))
		if err != nil {
			return err
		}
		if !bytes.Equal(copy, raw) {
			return domain.Fail("RESOURCE_CHANGED", "Fingerprint detail changed before publication")
		}
		return nil
	}, in.Publish)
	return err
}

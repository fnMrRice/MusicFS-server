package fingerprintworker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
)

func (s *Service) Indexes(ctx context.Context, owner, track, after string, limit int) ([]identification.FingerprintIndex, error) {
	return s.repo.FingerprintIndexes(ctx, owner, track, after, limit)
}

// Detail reads an explicitly requested, owner-scoped fingerprint. No fingerprint
// or original audio is uploaded to an external matching service.
func (s *Service) Detail(ctx context.Context, owner, id string) (fingerprint.Detail, error) {
	var detail fingerprint.Detail
	if err := ctx.Err(); err != nil {
		return detail, err
	}
	index, err := s.repo.FingerprintIndex(ctx, owner, id)
	if err != nil {
		return detail, err
	}
	in, err := s.media.Open(index.Object)
	if err != nil {
		return detail, s.invalidDetail(ctx, owner, id)
	}
	defer in.Close()
	decoder := json.NewDecoder(io.LimitReader(in, fingerprint.MaxDetailBytes+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&detail) != nil || decoder.Decode(new(any)) != io.EOF {
		return fingerprint.Detail{}, s.invalidDetail(ctx, owner, id)
	}
	if _, err = detail.Encode(); err != nil {
		return fingerprint.Detail{}, s.invalidDetail(ctx, owner, id)
	}
	if detail.Identity != index.ExtractorIdentity || detail.RequestedStartMs != index.StartMs || detail.RequestedDurationMs != index.DurationMs || detail.PCM.StartSample != index.ActualStartSample || detail.PCM.SampleCount != index.SampleCount || len(detail.Values) != index.ValueCount || detail.Quality != index.Quality {
		return fingerprint.Detail{}, s.invalidDetail(ctx, owner, id)
	}
	if err = ctx.Err(); err != nil {
		return fingerprint.Detail{}, err
	}
	return detail, nil
}

func (s *Service) invalidDetail(ctx context.Context, owner, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.repo.InvalidateFingerprint(ctx, owner, id); err != nil {
		slog.Error("Fingerprint detail invalidation failed", "fingerprintId", id, "errorCode", failureCode(err))
	}
	return domain.Fail("RESOURCE_CHANGED", "Fingerprint detail is missing or damaged; recompute it with a new operation")
}

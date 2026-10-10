package matching

import (
	"context"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
)

func usable(index identification.FingerprintIndex, source CandidateSource) bool {
	a := source.Input.Asset
	return index.Current && index.Quality.Status == "available" && index.SourceTrackID == a.SourceTrackID && index.AssetID == a.AssetID && index.AssetVersion == a.AssetVersion
}

func compatible(a, b identification.FingerprintIndex) bool {
	return a.FingerprintRequest == b.FingerprintRequest
}

// Choose one shared configuration/window before reading files. The bound is
// eight indexes per source; unrelated historical profiles cannot multiply work.
func choose(snapshot Snapshot) *identification.FingerprintIndex {
	var best *identification.FingerprintIndex
	bestCount := 0
	for _, anchor := range snapshot.Anchor.Fingerprints[:min(8, len(snapshot.Anchor.Fingerprints))] {
		if !usable(anchor, snapshot.Anchor) {
			continue
		}
		count := 0
		for _, source := range snapshot.Sources {
			for _, index := range source.Fingerprints[:min(8, len(source.Fingerprints))] {
				if usable(index, source) && compatible(anchor, index) {
					count++
					break
				}
			}
		}
		if count > bestCount || (count == bestCount && count > 0 && (anchor.DurationMs > best.DurationMs || (anchor.DurationMs == best.DurationMs && anchor.ID < best.ID))) {
			copy := anchor
			best, bestCount = &copy, count
		}
	}
	return best
}

func (s *Service) inputs(ctx context.Context, owner string, snapshot Snapshot) (candidates.Input, []candidates.Input, error) {
	anchor := snapshot.Anchor.Input
	anchor.Fingerprint = nil
	others := make([]candidates.Input, 0, len(snapshot.Sources))
	selected := choose(snapshot)
	if selected != nil && s.details != nil {
		evidence, err := s.load(ctx, owner, snapshot.Anchor, *selected)
		if err != nil {
			return anchor, nil, err
		}
		anchor.Fingerprint = evidence
	}
	for _, source := range snapshot.Sources {
		input := source.Input
		input.Fingerprint = nil
		if anchor.Fingerprint != nil {
			for _, index := range source.Fingerprints[:min(8, len(source.Fingerprints))] {
				if !usable(index, source) || !compatible(*selected, index) {
					continue
				}
				evidence, err := s.load(ctx, owner, source, index)
				if err != nil {
					return anchor, nil, err
				}
				input.Fingerprint = evidence
				break
			}
		}
		others = append(others, input)
	}
	return anchor, others, nil
}

func (s *Service) load(ctx context.Context, owner string, source CandidateSource, index identification.FingerprintIndex) (*candidates.FingerprintEvidence, error) {
	detail, err := s.details.Detail(ctx, owner, index.ID)
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) && (de.Code == "NOT_FOUND" || de.Code == "RESOURCE_CHANGED" || de.Code == "UNSUPPORTED") {
			return nil, nil
		}
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if _, err = detail.Encode(); err != nil || !sameDetail(detail, index) {
		return nil, domain.Fail("RESOURCE_CHANGED", "Fingerprint evidence changed")
	}
	return &candidates.FingerprintEvidence{Asset: source.Input.Asset, EvidenceID: index.ID, Detail: detail}, nil
}

func sameDetail(d fingerprint.Detail, i identification.FingerprintIndex) bool {
	return d.Identity == i.ExtractorIdentity && d.RequestedStartMs == i.StartMs && d.RequestedDurationMs == i.DurationMs && d.PCM.StartSample == i.ActualStartSample && d.PCM.SampleCount == i.SampleCount && len(d.Values) == i.ValueCount && d.Quality == i.Quality
}

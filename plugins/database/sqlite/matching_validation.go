package sqlite

import (
	"context"
	"math"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func validateCandidate(c candidates.Candidate, left, right matching.CandidateSource, anchor string) error {
	invalid := func() error { return domain.Fail("INVALID_ARGUMENT", "Invalid candidate evidence") }
	if c.Left.SourceTrackID >= c.Right.SourceTrackID || (c.Left.SourceTrackID != anchor && c.Right.SourceTrackID != anchor) || c.Left != left.Input.Asset || c.Right != right.Input.Asset || left.BindingRevision == "" || right.BindingRevision == "" || c.AlgorithmVersion != candidates.AlgorithmVersion || !c.RequiresConfirmation || c.Score < 1 || c.Score > 100 || len(c.Reasons) < 1 || len(c.Reasons) > 5 {
		return invalid()
	}
	seen := map[string]bool{}
	strong, fingerprint, metadata := false, false, false
	for _, reason := range c.Reasons {
		if seen[reason.Kind] {
			return invalid()
		}
		seen[reason.Kind] = true
		switch reason.Kind {
		case "verifiedFileSHA512Equal":
			l, r := left.Input.FileHash, right.Input.FileHash
			if l == nil || r == nil || l.SHA512 != r.SHA512 || l.FileSizeBytes != r.FileSizeBytes || reason.LeftEvidenceID != l.EvidenceID || reason.RightEvidenceID != r.EvidenceID || reason.Fingerprint != nil || reason.DurationDifferenceMs != nil || c.Score != 100 || len(c.Reasons) != 1 {
				return invalid()
			}
			strong = true
		case "fingerprintSimilar":
			var l, r *identification.FingerprintIndex
			for _, index := range left.Fingerprints {
				if index.ID == reason.LeftEvidenceID {
					l = &index
				}
			}
			for _, index := range right.Fingerprints {
				if index.ID == reason.RightEvidenceID {
					r = &index
				}
			}
			m := reason.Fingerprint
			if l == nil || r == nil || m == nil || reason.DurationDifferenceMs != nil || c.Score < 70 || c.Score > 99 || m.ComparedItems < 1 || m.ComparedItems > min(l.ValueCount, r.ValueCount) || m.ComparedBits != 32*m.ComparedItems || m.BitErrors < 0 || m.BitErrors > m.ComparedBits || m.OffsetItems < -2048 || m.OffsetItems > 2048 || math.IsNaN(m.BER) || math.IsInf(m.BER, 0) || math.Abs(m.BER-float64(m.BitErrors)/float64(m.ComparedBits)) > 1e-12 || m.BER > 0.08 || l.ExtractorIdentity != r.ExtractorIdentity || l.StartMs != r.StartMs || l.DurationMs != r.DurationMs || min(l.SampleCount, r.SampleCount)*5 < max(l.SampleCount, r.SampleCount)*4 {
				return invalid()
			}
			fingerprint = true
		case "metadataOnly", "titleEqual", "artistOverlap", "durationClose":
			if reason.LeftEvidenceID != "" || reason.RightEvidenceID != "" || reason.Fingerprint != nil {
				return invalid()
			}
			if reason.Kind == "durationClose" {
				if reason.DurationDifferenceMs == nil || *reason.DurationDifferenceMs < 0 || *reason.DurationDifferenceMs > 2000 {
					return invalid()
				}
			} else if reason.DurationDifferenceMs != nil {
				return invalid()
			}
			metadata = metadata || reason.Kind == "metadataOnly"
		default:
			return invalid()
		}
	}
	if !strong && !fingerprint && (!metadata || c.Score != 35 || !seen["titleEqual"] || !seen["artistOverlap"] || !seen["durationClose"]) {
		return invalid()
	}
	if !fingerprint {
		expected, err := candidates.Compare(context.Background(), left.Input, right.Input)
		if err != nil || expected == nil || !reflect.DeepEqual(*expected, c) {
			return invalid()
		}
	}
	return nil
}

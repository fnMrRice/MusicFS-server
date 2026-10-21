package standard

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
)

// Compare 评分一对已授权资产；排除项和人工决定由宿主在调用前处理。
func Compare(ctx context.Context, left, right Input) (*Candidate, error) {
	operations := 0
	return compare(ctx, left, right, &operations)
}

func compare(ctx context.Context, left, right Input, operations *int) (*Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := candidates.ValidateInput(left); err != nil {
		return nil, err
	}
	if err := candidates.ValidateInput(right); err != nil {
		return nil, err
	}
	if left.Asset.SourceTrackID == right.Asset.SourceTrackID {
		return nil, nil
	}
	// 固定资产顺序，保证分数、原因和偏移符号一致。
	if left.Asset.SourceTrackID > right.Asset.SourceTrackID {
		left, right = right, left
	}
	lh, err := candidates.CurrentHash(left)
	if err != nil {
		return nil, err
	}
	rh, err := candidates.CurrentHash(right)
	if err != nil {
		return nil, err
	}
	result := &Candidate{Left: left.Asset, Right: right.Asset, AlgorithmVersion: AlgorithmVersion, RequiresConfirmation: true, Reasons: []Reason{}}
	if lh != nil && rh != nil && lh.SHA512 == rh.SHA512 && lh.FileSizeBytes == rh.FileSizeBytes {
		result.Score = 100
		result.Reasons = append(result.Reasons, Reason{Kind: "verifiedFileSHA512Equal", LeftEvidenceID: lh.EvidenceID, RightEvidenceID: rh.EvidenceID})
		return result, nil
	}
	lf, err := candidates.CurrentFingerprint(left)
	if err != nil {
		return nil, err
	}
	rf, err := candidates.CurrentFingerprint(right)
	if err != nil {
		return nil, err
	}
	if lf != nil && rf != nil && candidates.Compatible(lf.Detail, rf.Detail) {
		metric, err := compareFingerprint(ctx, lf.Detail.Values, rf.Detail.Values, operations)
		if err != nil {
			return nil, err
		}
		// 8% is a conservative local review gate, not a measured error probability.
		if metric != nil {
			if int64(metric.BitErrors)*100 > int64(metric.ComparedBits)*8 {
				return nil, nil
			}
			result.Score = 90 - 250*metric.BitErrors/metric.ComparedBits
			result.Reasons = append(result.Reasons, Reason{Kind: "fingerprintSimilar", LeftEvidenceID: lf.EvidenceID, RightEvidenceID: rf.EvidenceID, Fingerprint: metric})
		}
	}
	title, artist, delta := candidates.Metadata(left, right)
	if result.Score == 0 {
		if !title || !artist || delta == nil || *delta > 2000 {
			return nil, nil
		}
		result.Score = 35
		result.Reasons = append(result.Reasons, Reason{Kind: "metadataOnly"})
	} else {
		if title {
			result.Score += 4
		}
		if artist {
			result.Score += 3
		}
		if delta != nil && *delta <= 2000 {
			result.Score += 2
		}
		result.Score = min(result.Score, 99)
	}
	if title {
		result.Reasons = append(result.Reasons, Reason{Kind: "titleEqual"})
	}
	if artist {
		result.Reasons = append(result.Reasons, Reason{Kind: "artistOverlap"})
	}
	if delta != nil && *delta <= 2000 {
		result.Reasons = append(result.Reasons, Reason{Kind: "durationClose", DurationDifferenceMs: delta})
	}
	return result, nil
}

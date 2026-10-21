package candidates

import (
	"context"
	"fmt"
)

// Scorer 只接收宿主已授权且去除人工排除项的证据，返回待确认候选。
type Scorer interface {
	Find(context.Context, Input, []Input) ([]Candidate, error)
}

func pairKey(left, right string) [2]string {
	if left > right {
		left, right = right, left
	}
	return [2]string{left, right}
}

// Find 保留输入限额、人工排除和结果绑定校验，将排序评分交给注入的插件。
func Find(ctx context.Context, scorer Scorer, anchor Input, others []Input, blocked []Pair) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ValidateInput(anchor); err != nil {
		return nil, err
	}
	if len(others) > MaxInputs || len(blocked) > 4096 {
		return nil, fmt.Errorf("RATE_LIMITED: candidate input limit exceeded")
	}
	exclusions := make(map[[2]string]bool, len(blocked))
	for _, pair := range blocked {
		if !textValid(pair.LeftSourceTrackID, 128, true) || !textValid(pair.RightSourceTrackID, 128, true) {
			return nil, fmt.Errorf("INVALID_ARGUMENT: invalid excluded candidate pair")
		}
		exclusions[pairKey(pair.LeftSourceTrackID, pair.RightSourceTrackID)] = true
	}
	filtered := make([]Input, 0, len(others))
	allowed := map[string]AssetRef{anchor.Asset.SourceTrackID: anchor.Asset}
	for _, other := range others {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if exclusions[pairKey(anchor.Asset.SourceTrackID, other.Asset.SourceTrackID)] || anchor.Asset.SourceTrackID == other.Asset.SourceTrackID {
			continue
		}
		if _, exists := allowed[other.Asset.SourceTrackID]; exists {
			return nil, fmt.Errorf("INVALID_ARGUMENT: duplicate candidate source")
		}
		if err := ValidateInput(other); err != nil {
			return nil, err
		}
		allowed[other.Asset.SourceTrackID] = other.Asset
		filtered = append(filtered, other)
	}
	if scorer == nil {
		return nil, fmt.Errorf("UNSUPPORTED: candidate scoring plugin is unavailable")
	}
	results, err := scorer.Find(ctx, anchor, filtered)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(results) > MaxResults {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: candidate scoring result limit exceeded")
	}
	seen := map[[2]string]bool{}
	for _, result := range results {
		left, leftOK := allowed[result.Left.SourceTrackID]
		right, rightOK := allowed[result.Right.SourceTrackID]
		key := pairKey(result.Left.SourceTrackID, result.Right.SourceTrackID)
		if !leftOK || !rightOK || result.Left != left || result.Right != right || result.Left.SourceTrackID >= result.Right.SourceTrackID ||
			(result.Left != anchor.Asset && result.Right != anchor.Asset) || seen[key] || !result.RequiresConfirmation ||
			result.AlgorithmVersion != AlgorithmVersion || result.Score < 1 || result.Score > 100 || len(result.Reasons) < 1 || len(result.Reasons) > 5 {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid candidate scoring result")
		}
		seen[key] = true
	}
	return results, nil
}

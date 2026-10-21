package candidates

import (
	"context"
	"fmt"
	"sort"
)

func pairKey(left, right string) [2]string {
	if left > right {
		left, right = right, left
	}
	return [2]string{left, right}
}

// Find compares one anchor with a bounded, already authorized set. Excluded or
// manually conflicting pairs are removed before evidence is examined. It does
// not scan a library or compute all pairs, and never leaks skipped source IDs.
func Find(ctx context.Context, anchor Input, others []Input, blocked []Pair) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateInput(anchor); err != nil {
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
	results := []Candidate{}
	seen := map[string]bool{}
	operations := 0
	for _, other := range others {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if exclusions[pairKey(anchor.Asset.SourceTrackID, other.Asset.SourceTrackID)] || anchor.Asset.SourceTrackID == other.Asset.SourceTrackID {
			continue
		}
		if seen[other.Asset.SourceTrackID] {
			return nil, fmt.Errorf("INVALID_ARGUMENT: duplicate candidate source")
		}
		seen[other.Asset.SourceTrackID] = true
		item, err := compare(ctx, anchor, other, &operations)
		if err != nil {
			return nil, err
		}
		if item != nil {
			results = append(results, *item)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Left.SourceTrackID != results[j].Left.SourceTrackID {
			return results[i].Left.SourceTrackID < results[j].Left.SourceTrackID
		}
		return results[i].Right.SourceTrackID < results[j].Right.SourceTrackID
	})
	if len(results) > MaxResults {
		results = results[:MaxResults]
	}
	return results, nil
}

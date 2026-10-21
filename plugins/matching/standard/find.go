package standard

import (
	"context"
	"fmt"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
)

// Find 对宿主提供的有限候选集计算分数并按固定顺序排序。
func (Plugin) Find(ctx context.Context, anchor Input, others []Input) (found []Candidate, findErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.matching.standard", "")
	finish := pluginlog.Operation(ctx, "matching.find")
	defer func() { finish(findErr) }()
	pluginlog.Logger(ctx).Log(ctx, pluginlog.LevelTrace, "Candidate comparison started", "candidateCount", len(others))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := candidates.ValidateInput(anchor); err != nil {
		return nil, err
	}
	if len(others) > MaxInputs {
		return nil, fmt.Errorf("RATE_LIMITED: candidate input limit exceeded")
	}
	results := []Candidate{}
	seen := map[string]bool{}
	operations := 0
	for _, other := range others {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if anchor.Asset.SourceTrackID == other.Asset.SourceTrackID {
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
	pluginlog.Logger(ctx).DebugContext(ctx, "Candidate comparison completed", "candidateCount", len(others), "resultCount", len(results), "comparisons", operations)
	return results, nil
}

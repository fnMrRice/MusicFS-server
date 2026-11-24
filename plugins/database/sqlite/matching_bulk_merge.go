package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

type bulkMergeGroup struct {
	sources []string
	target  string
}

// 后台任务只传递统计，逐曲审计和完整幂等收据保留在存储端。
func (s *Store) BulkMergeSummary(ctx context.Context, owner string, request matching.BulkMergeRequest, key string) (matching.BulkMergeCounts, error) {
	result, err := s.BulkMerge(ctx, owner, request, key)
	return result.BulkMergeCounts, err
}

func (s *Store) BulkMerge(ctx context.Context, owner string, request matching.BulkMergeRequest, key string) (matching.BulkMergeResult, error) {
	result := matching.BulkMergeResult{}
	if err := request.Validate(); err != nil {
		return result, err
	}
	m, replayed, err := s.beginMatching(ctx, owner, "bulk-merge", key, request.ExpectedRevision, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if replayed {
		if err = json.Unmarshal(m.replay, &result); err != nil {
			return result, err
		}
		result.Mutation = m.result
		return result, nil
	}
	sources, err := readBulkMergeSources(ctx, m.tx, owner)
	if err != nil {
		return result, err
	}
	graph := newBulkMergeGraph(sources)
	if request.Mode == "score" {
		err = addBulkMergeCandidates(ctx, m.tx, owner, *request.MinimumScore, graph, sources)
	} else {
		addBulkMergeIdentities(graph, sources)
	}
	if err != nil {
		return result, err
	}
	groups, skipped, err := planBulkMerge(ctx, m.tx, owner, graph, sources)
	if err != nil {
		return result, err
	}
	result.SkippedGroups = skipped
	// 必须完成整个连通分量的排除检查后才写入，避免传递关系绕过人工决定。
	for _, group := range groups {
		target := group.target
		if target == "" {
			target, err = createRecording(ctx, m.tx, owner)
			if err != nil {
				return result, err
			}
		}
		if err = m.moveSources(ctx, "link", group.sources, target); err != nil {
			return result, err
		}
		result.MergedGroups++
		result.MergedSources += len(group.sources)
	}
	if result.MergedGroups == 0 {
		// 空操作仍保存幂等收据，但不会制造新的人工修订或审计项。
		m.result.Revision = request.ExpectedRevision
	}
	_, err = m.finishResponse(ctx, func(value matching.Mutation) any {
		result.Mutation = value
		return result
	})
	return result, err
}

func addBulkMergeCandidates(ctx context.Context, tx *sql.Tx, owner string, minimum int, graph *bulkMergeGraph, sources map[string]bulkMergeSource) error {
	cache := map[string]matching.CandidateSource{}
	after, score := "", 101
	for {
		// 按数据库游标遍历全部候选；每批只限制读取量，不截断全库结果。
		records, err := readMatchingCandidates(ctx, tx, owner, "", score, after, 100)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		for _, record := range records {
			candidate := record.Candidate
			after, score = candidate.ID, candidate.Score
			if score < minimum {
				return nil
			}
			left, right := candidate.Left.SourceTrackID, candidate.Right.SourceTrackID
			l, lok := sources[left]
			r, rok := sources[right]
			if !lok || !rok || !l.current || !r.current || l.unlinked || r.unlinked {
				continue
			}
			current, err := currentCandidate(ctx, tx, owner, record, cache)
			if err != nil {
				return err
			}
			if current {
				graph.match(left, right)
			}
		}
	}
}

func planBulkMerge(ctx context.Context, tx *sql.Tx, owner string, graph *bulkMergeGraph, sources map[string]bulkMergeSource) ([]bulkMergeGroup, int, error) {
	result := []bulkMergeGroup{}
	skipped := 0
	for _, ids := range graph.groups() {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		recordings := map[string]bool{}
		units, blocked := 0, false
		for _, id := range ids {
			source := sources[id]
			blocked = blocked || source.unlinked
			if source.recording == "" {
				units++
			} else {
				recordings[source.recording] = true
			}
		}
		if units+len(recordings) < 2 {
			continue
		}
		if blocked || len(ids) > matching.MaxSources {
			skipped++
			continue
		}
		if err := checkExclusions(ctx, tx, owner, ids); err != nil {
			var failure *domain.Error
			if errors.As(err, &failure) && failure.Code == "CONFLICT" {
				skipped++
				continue
			}
			return nil, 0, err
		}
		targets := make([]string, 0, len(recordings))
		for target := range recordings {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		group := bulkMergeGroup{sources: ids}
		if len(targets) > 0 {
			group.target = targets[0]
		}
		result = append(result, group)
	}
	return result, skipped, nil
}

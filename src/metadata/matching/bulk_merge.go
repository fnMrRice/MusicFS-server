package matching

import "github.com/fnMrRice/MusicFS-server/src/core/domain"

// BulkMergeRequest 表示用户明确发起的一次全库批量关联。
type BulkMergeRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Mode             string `json:"mode"`
	MinimumScore     *int   `json:"minimumScore,omitempty"`
}

type BulkMergeResult struct {
	Mutation
	BulkMergeCounts
}

type BulkMergeCounts struct {
	MergedGroups  int `json:"mergedGroups"`
	MergedSources int `json:"mergedSources"`
	SkippedGroups int `json:"skippedGroups"`
}

func (r BulkMergeRequest) Validate() error {
	if r.ExpectedRevision < 0 {
		return domain.Fail("INVALID_ARGUMENT", "A nonnegative expectedRevision is required")
	}
	switch r.Mode {
	case "score":
		if r.MinimumScore == nil || *r.MinimumScore < 0 || *r.MinimumScore > 100 {
			return domain.Fail("INVALID_ARGUMENT", "Score mode requires an integer minimumScore between 0 and 100")
		}
	case "title_artist":
		if r.MinimumScore != nil {
			return domain.Fail("INVALID_ARGUMENT", "Title and artist mode does not accept minimumScore")
		}
	default:
		return domain.Fail("INVALID_ARGUMENT", "Unsupported bulk merge mode")
	}
	return nil
}

package matching

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
)

type Repository interface {
	MatchingRevision(context.Context, string) (int64, error)
	CandidateSnapshot(context.Context, string, string, []string, int) (Snapshot, error)
	SaveCandidates(context.Context, string, Snapshot, []candidates.Candidate) (CandidatePage, error)
	Candidates(context.Context, string, string, string, int) (CandidatePage, error)
	Recording(context.Context, string, string) (Recording, error)
	GetLink(context.Context, string, string) (Link, error)
	ManualLink(context.Context, string, LinkRequest, string) (Mutation, error)
	BulkMerge(context.Context, string, BulkMergeRequest, string) (BulkMergeResult, error)
	Unlink(context.Context, string, UnlinkRequest, string) (Mutation, error)
	Split(context.Context, string, SplitRequest, string) (Mutation, error)
	SetExclusion(context.Context, string, ExclusionRequest, string) (Mutation, error)
	ChangesAfter(context.Context, string, int64, int) (ChangePage, error)
}

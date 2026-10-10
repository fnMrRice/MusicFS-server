package matching

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
)

type FingerprintDetails interface {
	Detail(context.Context, string, string) (fingerprint.Detail, error)
}

// Service adds bounded evidence comparison to transactional manual decisions.
// Repository methods retain their authorization, revision and idempotency checks.
type Service struct {
	Repository
	details FingerprintDetails
	gate    chan struct{}
}

func New(repo Repository, details FingerprintDetails) *Service {
	return &Service{Repository: repo, details: details, gate: make(chan struct{}, 1)}
}

func (s *Service) Refresh(ctx context.Context, owner, sourceID string, sourceIDs []string) (CandidatePage, error) {
	if len(sourceIDs) > MaxSources {
		return CandidatePage{}, domain.Fail("INVALID_ARGUMENT", "Too many candidate sources")
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		return CandidatePage{}, domain.Fail("RATE_LIMITED", "Candidate comparison is busy")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	snapshot, err := s.Repository.CandidateSnapshot(ctx, owner, sourceID, sourceIDs, MaxSources)
	if err != nil {
		return CandidatePage{}, err
	}
	if snapshot.Owner != owner || snapshot.Anchor.Input.Asset.SourceTrackID != sourceID || len(snapshot.Sources) > MaxSources {
		return CandidatePage{}, domain.Fail("RESOURCE_CHANGED", "Candidate snapshot changed")
	}
	anchor, others, err := s.inputs(ctx, owner, snapshot)
	if err != nil {
		return CandidatePage{}, err
	}
	items, err := candidates.Find(ctx, anchor, others, snapshot.Blocked)
	if err != nil {
		return CandidatePage{}, comparisonError(err)
	}
	// Storage rechecks metadata, evidence bindings and the manual revision in the
	// publication transaction. A slow refresh cannot undo an intervening decision.
	return s.Repository.SaveCandidates(ctx, owner, snapshot, items)
}

func comparisonError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, code := range []string{"INVALID_ARGUMENT", "RATE_LIMITED"} {
		if strings.HasPrefix(err.Error(), code+":") {
			return domain.Fail(code, "Candidate evidence could not be compared")
		}
	}
	return err
}

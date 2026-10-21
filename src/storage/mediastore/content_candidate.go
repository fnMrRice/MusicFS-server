package mediastore

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// ApplyCandidate protects the exact immutable object while the caller's
// selection transaction checks its candidate and commits. Replays need not run
// the guard; new selections must. Invalidation runs only after that transaction
// returns, never while it owns the single database connection.
func (s *ContentService) ApplyCandidate(ctx context.Context, owner, objectID string, apply func(Guard) (Selection, error)) (Selection, error) {
	if apply == nil {
		return Selection{}, domain.Fail("INVALID_ARGUMENT", "Candidate selection callback is required")
	}
	object, err := s.ContentRepository.ContentObject(ctx, owner, objectID)
	if err != nil {
		return Selection{}, err
	}
	invalid := false
	guard := func(publish func() error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		file, err := s.files.Open(object.Object)
		if err != nil {
			invalid = true
			return domain.Fail("RESOURCE_CHANGED", "Media content is missing or damaged")
		}
		defer file.Close()
		if err = ctx.Err(); err != nil {
			return err
		}
		return publish()
	}
	result, err := apply(guard)
	if invalid {
		if failure := s.ContentRepository.InvalidateContent(ctx, owner, object.ID); failure != nil {
			return result, failure
		}
	}
	return result, err
}

package mediastore

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// SelectContent checks actual immutable bytes inside the repository's reference
// guard. The repository replays completed operations before invoking the guard.
func (s *ContentService) SelectContent(ctx context.Context, owner string, request SelectRequest, key string) (Selection, error) {
	object, err := s.ContentRepository.ContentObject(ctx, owner, request.ObjectID)
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
	result, err := s.ContentRepository.SelectContentGuarded(ctx, owner, request, key, guard)
	if invalid {
		if failure := s.ContentRepository.InvalidateContent(ctx, owner, request.ObjectID); failure != nil {
			return result, failure
		}
	}
	return result, err
}

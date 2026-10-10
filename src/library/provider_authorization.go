package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// AuthorizeProvider validates configured ownership without requiring a running
// worker. Offline providers may still have explicit albums and retained media.
func (s *Service) AuthorizeProvider(ctx context.Context, owner, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	provider, ok := s.Runtime(id)
	if !ok || provider.Owner != owner {
		return domain.Fail("NOT_FOUND", "Provider not found")
	}
	return nil
}

package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// GroupedRepository 在同一事务内按人工录音关联建立完整成员的分页快照。
type GroupedRepository interface {
	ListGroupedTracks(context.Context, string, string, string, int) (Page, error)
}

func (s *Service) ListGrouped(ctx context.Context, user, provider, cursor string, limit int) (Page, error) {
	if provider != "" {
		r, ok := s.Runtime(provider)
		if !ok || r.Owner != user {
			return Page{}, domain.Fail("NOT_FOUND", "Provider not found")
		}
	}
	repo, ok := s.repo.(GroupedRepository)
	if !ok {
		return Page{}, domain.Fail("UNSUPPORTED", "Grouped library is unavailable")
	}
	return repo.ListGroupedTracks(ctx, user, provider, cursor, limit)
}

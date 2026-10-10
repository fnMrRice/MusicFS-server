package albums

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

type Media interface {
	ApplyCandidate(context.Context, string, string, func(mediastore.Guard) (mediastore.Selection, error)) (mediastore.Selection, error)
}
type ProviderAuthorizer interface {
	AuthorizeProvider(context.Context, string, string) error
}
type Service struct {
	Repository
	media     Media
	providers ProviderAuthorizer
}

func New(repository Repository, media Media, providers ProviderAuthorizer) *Service {
	return &Service{Repository: repository, media: media, providers: providers}
}

// Empty albums still belong to an explicitly configured owner/provider. This
// check needs no online worker; later reads and edits use persisted ownership.
func (s *Service) CreateSourceAlbum(ctx context.Context, owner string, r CreateSourceAlbumRequest, key string) (SourceAlbum, error) {
	if s.providers == nil || s.Repository == nil {
		return SourceAlbum{}, domain.Fail("UNSUPPORTED", "Album provider authorization is unavailable")
	}
	if err := s.providers.AuthorizeProvider(ctx, owner, r.ProviderInstanceID); err != nil {
		return SourceAlbum{}, err
	}
	return s.Repository.CreateSourceAlbum(ctx, owner, r, key)
}

// SelectCover reuses the immutable-media guard, including delayed invalidation
// after rollback. Album covers have their own result and revision namespace.
func (s *Service) SelectCover(ctx context.Context, owner string, r SelectCoverRequest, key string) (CoverSelection, error) {
	var result CoverSelection
	if s.media == nil || s.Repository == nil {
		return result, domain.Fail("UNSUPPORTED", "Album cover selection is unavailable")
	}
	_, err := s.media.ApplyCandidate(ctx, owner, r.ObjectID, func(guard mediastore.Guard) (mediastore.Selection, error) {
		var err error
		result, err = s.Repository.SelectCover(ctx, owner, r, key, guard)
		return mediastore.Selection{}, err
	})
	return result, err
}

package library

import (
	"context"
	"errors"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func (s *Service) ResolveHashTarget(ctx context.Context, owner, id string) (identification.HashTarget, error) {
	t, err := s.Track(ctx, owner, id)
	if err != nil {
		return identification.HashTarget{}, err
	}
	r, ok := s.runtimes[t.ProviderID]
	if !ok || r.Owner != owner {
		return identification.HashTarget{}, domain.Fail("NOT_FOUND", "Source track not found")
	}
	if !r.StableHash {
		return identification.HashTarget{}, domain.Fail("UNSUPPORTED", "Provider does not permit complete stable original-file hashing")
	}
	if r.Worker == nil {
		return identification.HashTarget{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	select {
	case <-r.Worker.Done():
		return identification.HashTarget{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	default:
	}
	if t.Availability != "available" || t.Asset == nil || !t.Asset.Current || t.Asset.FileSizeBytes == nil || *t.Asset.FileSizeBytes != t.Size {
		return identification.HashTarget{}, domain.Fail("RESOURCE_CHANGED", "Source asset is no longer current")
	}
	return identification.HashTarget{SourceTrackID: t.ID, AssetID: t.AssetID, AssetVersion: t.Version, FileSizeBytes: t.Size}, nil
}

func (s *Service) AutoHashScopes() []identification.HashScope {
	scopes := []identification.HashScope{}
	for _, r := range s.runtimes {
		if r.StableHash && r.Worker != nil {
			select {
			case <-r.Worker.Done():
				continue
			default:
			}
			scopes = append(scopes, identification.HashScope{Owner: r.Owner, ProviderInstanceID: r.ID})
		}
	}
	return scopes
}

type stableHashInput struct {
	*sourceaccess.StableReader
	handle                *sourceaccess.Handle
	service               *Service
	ctx                   context.Context
	owner, provider, task string
	target                identification.HashTarget
	once                  sync.Once
	closeError            error
}

func (h *stableHashInput) Validate() error {
	if err := h.StableReader.Validate(); err != nil {
		return err
	}
	current, err := h.service.ResolveHashTarget(h.ctx, h.owner, h.target.SourceTrackID)
	if err != nil {
		return err
	}
	if current != h.target {
		return domain.Fail("RESOURCE_CHANGED", "Hash source asset changed")
	}
	return nil
}

func (h *stableHashInput) Close() error {
	h.once.Do(func() {
		h.closeError = errors.Join(h.StableReader.Close(), h.handle.Close())
		h.service.Revoke(h.provider, h.task)
	})
	return h.closeError
}

func (h *stableHashInput) Publish(publish func() error) error {
	return h.StableReader.Publish(func() error {
		current, err := h.service.ResolveHashTarget(h.ctx, h.owner, h.target.SourceTrackID)
		if err != nil {
			return err
		}
		if current != h.target {
			return domain.Fail("RESOURCE_CHANGED", "Hash source asset changed")
		}
		return publish()
	})
}

func (s *Service) OpenStableHash(ctx context.Context, owner, task string, target identification.HashTarget) (identification.StableInput, error) {
	current, err := s.ResolveHashTarget(ctx, owner, target.SourceTrackID)
	if err != nil {
		return nil, err
	}
	if current != target {
		return nil, domain.Fail("RESOURCE_CHANGED", "Hash source asset changed")
	}
	t, err := s.Track(ctx, owner, target.SourceTrackID)
	if err != nil {
		return nil, err
	}
	if t.AssetID != target.AssetID || t.Version != target.AssetVersion || t.Size != target.FileSizeBytes {
		return nil, domain.Fail("RESOURCE_CHANGED", "Hash source asset changed")
	}
	h, err := s.Open(ctx, t, task)
	if err != nil {
		s.Revoke(t.ProviderID, task)
		return nil, err
	}
	guard, err := h.Stable(ctx)
	if err != nil {
		h.Close()
		s.Revoke(t.ProviderID, task)
		return nil, err
	}
	return &stableHashInput{StableReader: guard, handle: h, service: s, ctx: ctx, owner: owner, provider: t.ProviderID, task: task, target: target}, nil
}

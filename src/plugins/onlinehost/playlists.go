package onlinehost

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type playlistRemote struct{ host *Service }

// PlaylistRemote reuses the scoped online workers; callers cannot supply URLs,
// platform credentials, transport settings, or a configuration revision.
func (s *Service) PlaylistRemote() playlists.Remote { return &playlistRemote{s} }

func (p *playlistRemote) Acquire(ctx context.Context, owner string, binding playlists.Binding) (context.Context, func(), error) {
	ctx, runtime, release, err := p.host.AcquireRuntime(ctx, owner, binding.InstanceID)
	if err != nil {
		return ctx, nil, err
	}
	if runtime.ConfigRevision != binding.ConfigRevision || runtime.Platform != binding.Platform {
		release()
		return ctx, nil, domain.Fail("RESOURCE_CHANGED", "Remote playlist binding changed")
	}
	view, err := platformSession(ctx, runtime)
	if err != nil {
		release()
		return ctx, nil, err
	}
	if binding.AccountID != view.AccountID {
		release()
		return ctx, nil, domain.Fail("RESOURCE_CHANGED", "Remote playlist account changed; import again")
	}
	return ctx, release, nil
}

func (p *playlistRemote) Authorize(ctx context.Context, owner string, ref playlists.RemoteRef) (playlists.Binding, error) {
	if err := ref.Validate(); err != nil || !externalID.MatchString(ref.ExternalID) {
		return playlists.Binding{}, playlists.Invalid()
	}
	runtime, err := p.host.Runtime(ctx, owner, ref.InstanceID, ref.AccountID)
	if err != nil {
		return playlists.Binding{}, err
	}
	view, err := platformSession(ctx, runtime)
	if err != nil {
		return playlists.Binding{}, err
	}
	if ref.AccountID == "" && runtime.descriptor.Authentication != "anonymous" {
		return playlists.Binding{}, domain.Fail("RESOURCE_CHANGED", "Select the current platform account and import again")
	}
	if ref.AccountID != view.AccountID {
		return playlists.Binding{}, domain.Fail("ACCESS_DENIED", "Platform account does not match the playlist")
	}
	supported := false
	for _, capability := range runtime.descriptor.Capabilities {
		if capability.Name == "playlist.observe" && capability.Supported {
			supported = true
		}
	}
	if !supported {
		return playlists.Binding{}, domain.Fail("UNSUPPORTED", "Public playlist import is not supported by this instance")
	}
	validID := online.ReferenceURL(runtime.descriptor.Resources.Playlists, ref.ExternalID) != ""
	if !validID {
		return playlists.Binding{}, playlists.Invalid()
	}
	return playlists.Binding{RemoteRef: ref, Platform: runtime.Platform, ConfigRevision: runtime.ConfigRevision}, nil
}

func (p *playlistRemote) ObservePlaylist(ctx context.Context, owner string, binding playlists.Binding) (playlists.Observation, error) {
	current, err := p.Authorize(ctx, owner, binding.RemoteRef)
	if err != nil {
		return playlists.Observation{}, err
	}
	if current != binding {
		return playlists.Observation{}, domain.Fail("RESOURCE_CHANGED", "Remote playlist instance configuration changed")
	}
	observation, err := p.host.ObservePlaylist(ctx, owner, binding.InstanceID, binding.ExternalID)
	if err != nil {
		return playlists.Observation{}, err
	}
	result := playlists.Observation{PlaylistObservation: observation}
	if err := result.Validate(binding.ExternalID); err != nil {
		return playlists.Observation{}, err
	}
	ids := make([]string, 0, len(observation.Entries))
	seen := map[string]bool{}
	for _, entry := range observation.Entries {
		if !seen[entry.ExternalTrackID] {
			seen[entry.ExternalTrackID] = true
			ids = append(ids, entry.ExternalTrackID)
		}
	}
	for start := 0; start < len(ids); start += online.MaxPageItems {
		requested := ids[start:min(start+online.MaxPageItems, len(ids))]
		batch, err := p.host.MetadataBatchForAccount(ctx, owner, binding.InstanceID, binding.AccountID, requested)
		if err != nil {
			return playlists.Observation{}, err
		}
		if len(batch.Items) != len(requested) {
			return playlists.Observation{}, domain.Fail("PROTOCOL_MISMATCH", "Playlist metadata batch has the wrong size")
		}
		for i, item := range batch.Items {
			if item.ExternalID != requested[i] {
				return playlists.Observation{}, domain.Fail("PROTOCOL_MISMATCH", "Playlist metadata batch has an unexpected identity")
			}
		}
		result.Tracks = append(result.Tracks, batch.Items...)
	}
	return result, nil
}

package onlinehost

import (
	"context"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

type playlistRemote struct{ host *Service }

var playlistExternalID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

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
	if ref.AccountID == "" {
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
	if !playlistExternalID.MatchString(ref.ExternalID) {
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
	return playlists.Observation{PlaylistObservation: observation}, nil
}

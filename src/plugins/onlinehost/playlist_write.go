package onlinehost

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func playlistWriteAccess(ctx context.Context, r Runtime, ref playlists.RemoteRef) (provider.PlaylistTrackWriter, error) {
	if ref.Validate() != nil || ref.AccountID == "" || online.ReferenceURL(r.descriptor.Resources.Playlists, ref.ExternalID) == "" {
		return nil, playlists.Invalid()
	}
	view, err := platformSession(ctx, r)
	if err != nil {
		return nil, err
	}
	if view.AccountID != ref.AccountID {
		return nil, domain.Fail("ACCESS_DENIED", "Platform account does not match the linked playlist")
	}
	if writer, ok := r.Provider.(provider.PlaylistTrackWriter); ok {
		for _, capability := range r.descriptor.Capabilities {
			if capability.Name == "playlist.addTracks" && capability.Supported {
				return writer, nil
			}
		}
	}
	return nil, domain.Fail("UNSUPPORTED", "This source does not support playlist updates")
}

func (s *Service) AuthorizePlaylistWrite(ctx context.Context, owner string, ref playlists.RemoteRef) (playlists.Binding, error) {
	return call(ctx, s, owner, ref.InstanceID, func(ctx context.Context, r Runtime) (playlists.Binding, error) {
		if _, err := playlistWriteAccess(ctx, r, ref); err != nil {
			return playlists.Binding{}, err
		}
		observation, err := r.Provider.ObservePlaylist(ctx, ref.ExternalID)
		if err != nil {
			return playlists.Binding{}, err
		}
		if observation.Playlist.ExternalID != ref.ExternalID || !observation.Playlist.Writable {
			return playlists.Binding{}, domain.Fail("ACCESS_DENIED", "Only a playlist owned by this account can be linked")
		}
		return playlists.Binding{RemoteRef: ref, Platform: r.Platform, ConfigRevision: r.ConfigRevision}, nil
	})
}

func (s *Service) AddPlaylistTrack(ctx context.Context, owner string, ref playlists.RemoteRef, trackID string) error {
	_, err := call(ctx, s, owner, ref.InstanceID, func(ctx context.Context, r Runtime) (struct{}, error) {
		writer, err := playlistWriteAccess(ctx, r, ref)
		if err != nil {
			return struct{}{}, err
		}
		if online.ReferenceURL(r.descriptor.Resources.Tracks, trackID) == "" {
			return struct{}{}, playlists.Invalid()
		}
		err = writer.AddPlaylistTrack(ctx, online.PlaylistTrackAddition{AccountID: ref.AccountID, ExternalID: ref.ExternalID, TrackID: trackID})
		return struct{}{}, err
	})
	return err
}

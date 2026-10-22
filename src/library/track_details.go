package library

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type TrackDetails struct {
	SourceTrackID      string `json:"sourceTrackId"`
	ProviderInstanceID string `json:"providerInstanceId"`
	pluginproto.TrackDetails
}

func (s *Service) Details(ctx context.Context, owner, id string) (TrackDetails, error) {
	track, err := s.repo.FindTrack(ctx, owner, id)
	if err != nil {
		return TrackDetails{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	lease, err := s.HoldRead(ctx, owner, track.ProviderID)
	if err != nil {
		return TrackDetails{}, err
	}
	defer lease.Close()
	w := lease.runtime.Worker
	if w == nil {
		return TrackDetails{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	select {
	case <-w.Done():
		return TrackDetails{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	default:
	}
	check := func() error {
		entry, err := w.Access.Inspect(track.RootID, track.Path)
		if err != nil {
			return domain.Fail(pluginproto.ErrorCode(err), "Source is unavailable")
		}
		if entry.ObservedVersion != track.Version {
			return domain.Fail("RESOURCE_CHANGED", "Source changed; rescan the library")
		}
		return nil
	}
	if err := check(); err != nil {
		return TrackDetails{}, err
	}
	value, err := w.TrackDetails(lease.Context(), pluginproto.ResolveRequest{ExternalID: track.ExternalID, ExpectedVersion: track.Version})
	if err != nil {
		if lease.Context().Err() != nil {
			return TrackDetails{}, lease.Context().Err()
		}
		return TrackDetails{}, domain.Fail(pluginproto.ErrorCode(err), "Provider track details request failed")
	}
	if err := check(); err != nil {
		return TrackDetails{}, err
	}
	return TrackDetails{SourceTrackID: track.ID, ProviderInstanceID: track.ProviderID, TrackDetails: value}, nil
}

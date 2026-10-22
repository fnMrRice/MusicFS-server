package onlinehost

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type trackDetailsProvider interface {
	TrackDetails(context.Context, string) (pluginproto.TrackDetails, error)
}

func (s *Service) TrackDetails(ctx context.Context, owner, id, external string) (pluginproto.TrackDetails, error) {
	if !externalID.MatchString(external) {
		return pluginproto.TrackDetails{}, domain.Fail("INVALID_ARGUMENT", "Invalid external track identity")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (pluginproto.TrackDetails, error) {
		provider, ok := r.Provider.(trackDetailsProvider)
		if !ok {
			return pluginproto.TrackDetails{}, domain.Fail("UNSUPPORTED", "Provider does not support track details")
		}
		details, err := provider.TrackDetails(ctx, external)
		if err == nil && details.Validate(external) != nil {
			err = domain.Fail("PROTOCOL_MISMATCH", "Invalid provider track details")
		}
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		return details, nil
	})
}

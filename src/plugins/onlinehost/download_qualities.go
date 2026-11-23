package onlinehost

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func (s *Service) DownloadQualities(ctx context.Context, owner, id string) (pluginproto.QualityOptions, error) {
	r, err := s.Runtime(ctx, owner, id, "")
	if err != nil {
		return pluginproto.QualityOptions{}, err
	}
	if !supportsAudio(r) {
		return pluginproto.QualityOptions{}, domain.Fail("UNSUPPORTED", "Audio downloads are not available for this source")
	}
	if _, err := platformSession(ctx, r); err != nil {
		return pluginproto.QualityOptions{}, err
	}
	if options := r.descriptor.DownloadQualities; options != nil {
		return *options, nil
	}
	return pluginproto.QualityOptions{Options: []pluginsettings.Option{
		{Value: "", Label: "Source default", LocalizedLabels: map[string]string{"zh": "来源默认音质", "zh-Hant": "來源預設音質", "ja": "配信元の既定音質"}},
	}}, nil
}

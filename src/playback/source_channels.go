package playback

import (
	"context"
	"os"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// 只探测已经固定的解封装快照，避免上游多声道先被转成立体声再交给 Web。
func (p *preparedInput) selectProcessingProfile(ctx context.Context, capabilities Capabilities, bitrate int64, processed, fixed bool) error {
	if p.processor == nil {
		if processed {
			return domain.Fail("UNSUPPORTED", "Audio processor is unavailable")
		}
		return nil
	}
	channels := p.profile.Channels
	if p.processor.ChannelMixDisabled() && processed {
		file, err := os.Open(p.path)
		if err != nil {
			return err
		}
		metadata, err := p.processor.ProbeOutputMetadata(ctx, file, p.size)
		file.Close()
		if err != nil || metadata.Channels < 1 {
			return domain.Fail("UNSUPPORTED", "Source channel layout is unavailable")
		}
		channels = metadata.Channels
	}
	if !fixed {
		surround, _ := audioffmpeg.ProfileByID("opus-5.1")
		selected := p.processor.WithSourceChannels(channels, supportsOutputProfile(capabilities, surround))
		selected, err := selected.WithSourceBitrate(bitrate)
		if err == nil {
			p.processor = selected
		} else if processed {
			return domain.Fail("UNSUPPORTED", "No compatible audio processing profile is available")
		}
	}
	if !processed {
		return nil
	}
	profile, err := p.processor.OutputProfile()
	if err != nil || !supportsOutputProfile(capabilities, profile) {
		return domain.Fail("UNSUPPORTED", "No compatible audio processing profile is available")
	}
	// 跨源恢复固定编码身份；不同声道的替代来源不能暗中恢复声道扩展。
	if fixed && p.processor.ChannelMixDisabled() && channels < profile.Channels {
		return domain.Fail("RESTART_REQUIRED", "Replacement source has fewer channels than the active playback")
	}
	p.profile = profile
	return nil
}

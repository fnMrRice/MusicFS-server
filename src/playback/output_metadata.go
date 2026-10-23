package playback

import (
	"context"
	"log/slog"
	"os"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
)

func (p *preparedInput) inspectOriginalProfile(ctx context.Context, track *library.Track) error {
	if track.MIME == "audio/wav" {
		return inspectWave(p.path, track, &p.profile)
	}
	if track.MIME == "audio/flac" {
		info, err := audiocodecs.InspectNative("audio/flac", p.path, p.size)
		if err != nil {
			return err
		}
		p.profile.Channels = info.Channels
		if info.SampleRateHz != nil {
			p.profile.SampleRate = int(*info.SampleRateHz)
		}
		if info.DurationMs != nil {
			track.DurationMs = info.DurationMs
		}
	}
	setOriginalLayout(&p.profile)
	file, err := os.Open(p.path)
	if err != nil {
		return err
	}
	defer file.Close()
	// 此时快照已解除封装；探测实际交付字节，不读原文件或重新扫描音乐库。
	metadata, err := p.processor.ProbeOutputMetadata(ctx, file, p.size)
	if err != nil {
		slog.DebugContext(ctx, "Original playback metadata unavailable", "error", err)
		return ctx.Err()
	}
	if metadata.Channels >= 1 && metadata.Channels <= 8 {
		p.profile.Channels = metadata.Channels
		if len(metadata.Layout) <= 128 {
			p.profile.Layout = metadata.Layout
		}
		setOriginalLayout(&p.profile)
	}
	if metadata.SampleRateHz != nil {
		p.profile.SampleRate = int(*metadata.SampleRateHz)
	}
	if metadata.BitrateBps != nil && *metadata.BitrateBps > 0 {
		p.profile.BitrateBps = int(*metadata.BitrateBps)
	}
	return ctx.Err()
}

func setOriginalLayout(profile *audiotypes.Profile) {
	if profile.Channels == 1 {
		profile.Layout = "mono"
	} else if profile.Channels == 2 {
		profile.Layout = "stereo"
	}
}

package playback

import (
	"context"
	"log/slog"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (p *preparedInput) inspectOriginalProfile(ctx context.Context, track *library.Track) error {
	if track.MIME == "audio/wav" {
		return inspectWave(p.path, track, &p.profile)
	}
	file, err := os.Open(p.path)
	if err != nil {
		return err
	}
	defer file.Close()
	// 此时快照已解除 NCM 封装；探测实际交付字节，不读原文件或重新扫描音乐库。
	metadata, err := p.processor.ProbeOutputMetadata(ctx, file, p.size)
	if err != nil {
		slog.DebugContext(ctx, "Original playback metadata unavailable", "error", err)
		return ctx.Err()
	}
	if metadata.Channels == 1 || metadata.Channels == 2 {
		p.profile.Channels = metadata.Channels
		p.profile.Layout = "stereo"
		if metadata.Channels == 1 {
			p.profile.Layout = "mono"
		}
	}
	if metadata.SampleRateHz != nil {
		p.profile.SampleRate = int(*metadata.SampleRateHz)
	}
	return ctx.Err()
}

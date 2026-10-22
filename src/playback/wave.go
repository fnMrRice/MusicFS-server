package playback

import (
	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

// inspectWave 将插件验证后的原生音频属性应用到播放表示。
func inspectWave(path string, track *library.Track) error {
	info, err := audioffmpeg.InspectNativeWave(path, track.Size)
	if err != nil {
		return err
	}
	track.Codec, track.Container = &info.Codec, &info.Container
	track.DurationMs, track.BitrateBps = &info.DurationMs, &info.BitrateBps
	return nil
}

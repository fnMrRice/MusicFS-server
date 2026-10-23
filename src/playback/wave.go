package playback

import (
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
)

// inspectWave 将插件验证后的原生音频属性应用到播放表示。
func inspectWave(path string, track *library.Track, profile *audiotypes.Profile) error {
	info, err := audiocodecs.InspectNative("audio/wav", path, track.Size)
	if err != nil {
		return err
	}
	track.Codec, track.Container = info.Codec, info.Container
	track.DurationMs, track.BitrateBps = info.DurationMs, info.BitrateBps
	profile.Channels = info.Channels
	if info.SampleRateHz != nil {
		profile.SampleRate = int(*info.SampleRateHz)
	}
	if info.BitrateBps != nil {
		profile.BitrateBps = int(*info.BitrateBps)
	}
	profile.Layout = "stereo"
	if info.Channels == 1 {
		profile.Layout = "mono"
	}
	return nil
}

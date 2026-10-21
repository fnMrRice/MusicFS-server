package audioffmpeg

import "fmt"

// ProfileByID 固定编码参数，避免播放描述、缓存身份与实际输出格式分离。
func ProfileByID(id string) (Profile, bool) {
	profile := Profile{ID: id, SampleRate: 48000, SeekMode: "timeRestart"}
	switch id {
	case "mp3-128":
		profile.MIME, profile.Codec, profile.Container = "audio/mpeg", "mp3", "mp3"
		profile.Layout, profile.Channels, profile.BitrateBps = "stereo", 2, 128000
	case "opus-5.1":
		profile.MIME, profile.Codec, profile.Container = "audio/webm", "opus", "webm"
		profile.Layout, profile.Channels, profile.BitrateBps = "5.1", 6, 256000
	case "opus-7.1":
		profile.MIME, profile.Codec, profile.Container = "audio/webm", "opus", "webm"
		profile.Layout, profile.Channels, profile.BitrateBps = "7.1", 8, 384000
	default:
		return Profile{}, false
	}
	return profile, true
}

// OutputProfile 只返回当前工具探测已确认支持的部署输出格式。
func (p *Processor) OutputProfile() (Profile, error) {
	if p == nil {
		return Profile{}, fmt.Errorf("UNSUPPORTED: audio processor is unavailable")
	}
	id := "mp3-128"
	switch p.config.OutputLayout {
	case "", "stereo":
	case "5.1":
		id = "opus-5.1"
	case "7.1":
		id = "opus-7.1"
	default:
		return Profile{}, fmt.Errorf("UNSUPPORTED: unknown output channel layout")
	}
	for _, supported := range p.Capabilities.Profiles {
		if supported.ID == id {
			profile, _ := ProfileByID(id)
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("UNSUPPORTED: required output encoding profile is unavailable")
}

// 环绕声部署始终处理音频；默认立体声仅在已需处理时转换。
func (p *Processor) RequiresChannelConversion() bool {
	return p != nil && p.config.OutputLayout != "" && p.config.OutputLayout != "stereo"
}

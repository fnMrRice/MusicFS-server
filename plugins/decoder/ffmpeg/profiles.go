package audioffmpeg

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

var mp3Bitrates = []int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}

// ProfileByID 固定编码参数，避免播放描述、缓存身份与实际输出格式分离。
func ProfileByID(id string) (Profile, bool) {
	profile := Profile{ID: id, SampleRate: 48000, SeekMode: "timeRestart"}
	if value, ok := strings.CutPrefix(id, "mp3-"); ok {
		kbps, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(kbps) != value || !slices.Contains(mp3Bitrates, kbps) {
			return Profile{}, false
		}
		profile.MIME, profile.Codec, profile.Container = "audio/mpeg", "mp3", "mp3"
		profile.Layout, profile.Channels, profile.BitrateBps = "stereo", 2, kbps*1000
		return profile, true
	}
	for _, layout := range []string{"5.1", "7.1"} {
		base := "opus-" + layout
		if id != base && !strings.HasPrefix(id, base+"-") {
			continue
		}
		profile.MIME, profile.Codec, profile.Container, profile.Layout = "audio/webm", "opus", "webm", layout
		profile.Channels, profile.BitrateBps = 6, 256000
		if layout == "7.1" {
			profile.Channels, profile.BitrateBps = 8, 384000
		}
		if id != base {
			value := strings.TrimPrefix(id, base+"-")
			bps, err := strconv.Atoi(value)
			if err != nil || strconv.Itoa(bps) != value || bps < 500 || bps > 256000*profile.Channels || bps == profile.BitrateBps {
				return Profile{}, false
			}
			profile.BitrateBps = bps
		}
		return profile, true
	}
	return Profile{}, false
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
			if p.selectedProfile != "" {
				selected, valid := ProfileByID(p.selectedProfile)
				if !valid || selected.Codec != profile.Codec || selected.Layout != profile.Layout {
					return Profile{}, fmt.Errorf("UNSUPPORTED: selected output profile differs from configured encoding")
				}
				return selected, nil
			}
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("UNSUPPORTED: required output encoding profile is unavailable")
}

// WithSourceBitrate 按原始音频码率创建会话视图，不修改部署或用户设置。
// MP3 选择最近标准档位（等距时选较高档）；Opus 在编码器允许范围内保留目标值。
func (p *Processor) WithSourceBitrate(bitrate int64) (*Processor, error) {
	profile, err := p.OutputProfile()
	if err != nil || bitrate <= 0 {
		return p, err
	}
	id := profile.ID
	if profile.Codec == "mp3" {
		bitrate = min(bitrate, 320000)
		nearest := mp3Bitrates[0]
		for _, kbps := range mp3Bitrates[1:] {
			if bitrate*2 < int64(nearest+kbps)*1000 {
				break
			}
			nearest = kbps
		}
		id = "mp3-" + strconv.Itoa(nearest)
	} else {
		bps := min(max(bitrate, 500), int64(256000*profile.Channels))
		id = "opus-" + profile.Layout
		base, _ := ProfileByID(id)
		if bps != int64(base.BitrateBps) {
			id += "-" + strconv.FormatInt(bps, 10)
		}
	}
	return &Processor{parent: p.runtime(), config: p.config, Capabilities: p.Capabilities, dspVersions: p.dspVersions,
		slots: p.slots, normalizeSlot: p.normalizeSlot, identity: p.identity, selectedProfile: id}, nil
}

// 环绕声部署始终处理音频；默认立体声仅在已需处理时转换。
func (p *Processor) RequiresChannelConversion() bool {
	return p != nil && p.config.OutputLayout != "" && p.config.OutputLayout != "stereo"
}

// Package quality 声明网易云客户端的播放音质和宿主设置字段。
package quality

import "github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"

const Default = "exhigh"

var levels = []struct{ value, english, chinese string }{
	{"sky", "Immersive Surround", "沉浸环绕声"},
	{"jymaster", "Studio Master", "超清母带"},
	{"jyeffect", "HD Audio", "高清臻音"},
	{"hires", "Hi-Res Lossless", "高解析度无损"},
	{"lossless", "Lossless", "无损"},
	{"exhigh", "Very High", "极高"},
	{"standard", "Standard", "标准"},
}

func Supported(value string) bool {
	for _, level := range levels {
		if level.value == value {
			return true
		}
	}
	return false
}

func Field() pluginsettings.Field {
	field := pluginsettings.Field{
		Key: "playbackQuality", Type: "select", Label: "Playback quality", DefaultValue: Default,
		Description:           "The available quality depends on the track and your NetEase account.",
		LocalizedLabels:       map[string]string{"zh": "播放音质"},
		LocalizedDescriptions: map[string]string{"zh": "实际音质取决于曲目版本和网易云账号权限。"},
	}
	for _, level := range levels {
		field.Options = append(field.Options, pluginsettings.Option{Value: level.value, Label: level.english, LocalizedLabels: map[string]string{"zh": level.chinese}})
	}
	return field
}

package ncmdecoder

import "github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"

// SettingsSchema 只声明 NCM 自身能提供的原始音频输出，不宣称支持转码。
func SettingsSchema() pluginsettings.Schema {
	return pluginsettings.Schema{Revision: "ncm-codec-settings-v1", Title: "NCM decoding", LocalizedTitles: map[string]string{"zh-CN": "NCM 解码"},
		Description:           "NCM preserves the original MP3 or FLAC payload. Further PCM decoding is provided by an audio codec plugin.",
		LocalizedDescriptions: map[string]string{"zh-CN": "NCM 保留内部原始 MP3 或 FLAC 音频；后续 PCM 解码由音频编解码插件提供。"},
		Fields: []pluginsettings.Field{{Key: "decodeOutput", Type: "select", Label: "Decoder output format", LocalizedLabels: map[string]string{"zh-CN": "解码输出格式"},
			Description: "The payload determines the format; the source file is not rewritten.", LocalizedDescriptions: map[string]string{"zh-CN": "输出格式由内部音频决定，不改写源文件。"},
			DefaultValue: "native", Options: []pluginsettings.Option{{Value: "native", Label: "Original audio (MP3 / FLAC)", LocalizedLabels: map[string]string{"zh-CN": "原始音频（MP3 / FLAC）"}}}}},
		Actions: []pluginsettings.Action{},
	}
}

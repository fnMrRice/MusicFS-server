package audioffmpeg

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
)

const (
	DecodeFloat32  = "pcm_f32le"
	DecodeSigned16 = "pcm_s16le"
)

// DecodeOutput 只接受插件声明的 PCM 格式；空值兼容已有调用者。
func DecodeOutput(value string) (string, error) {
	if value == "" {
		return DecodeFloat32, nil
	}
	if value != DecodeFloat32 && value != DecodeSigned16 {
		return "", fmt.Errorf("UNSUPPORTED: unsupported decoder output format")
	}
	return value, nil
}

func (p *Processor) SupportsDecodeOutput(value string) error {
	format, err := DecodeOutput(value)
	if err != nil {
		return err
	}
	if p == nil || !supportsDecodeOutput(p.Capabilities, format) {
		return fmt.Errorf("UNSUPPORTED: decoder output format is unavailable")
	}
	return nil
}

func supportsDecodeOutput(capabilities Capabilities, format string) bool {
	// 旧能力描述始终只声明原来的 float32 路径。
	return slices.Contains(capabilities.DecodeOutputs, format) || len(capabilities.DecodeOutputs) == 0 && format == DecodeFloat32
}

// ProcessingIdentityForDecodeOutput 将量化格式绑定到缓存及时间轴证据。
// 默认格式保留原身份，避免无配置变更时废弃已有证据。
func (p *Processor) ProcessingIdentityForDecodeOutput(value string) ProcessingIdentity {
	identity := p.ProcessingIdentity()
	format, err := DecodeOutput(value)
	if err != nil || identity.PipelineRevision == "" {
		return ProcessingIdentity{}
	}
	if format != DecodeFloat32 {
		digest := sha256.Sum256([]byte(identity.PipelineRevision + "\x00decode-output-v1\x00" + format))
		identity.PipelineRevision = fmt.Sprintf("dsp-v1:%x", digest)
	}
	return identity
}

func SettingsSchema() pluginsettings.Schema {
	return pluginsettings.Schema{
		Revision: "ffmpeg-codec-settings-v1", Title: "Audio decoding", LocalizedTitles: map[string]string{"zh-CN": "音频解码"},
		Description:           "Decoder output applies to new playback sessions. Fingerprint analysis uses its fixed normalization format.",
		LocalizedDescriptions: map[string]string{"zh-CN": "解码输出设置在新建的播放会话中生效。音频指纹保持固定的标准化格式。"},
		Fields: []pluginsettings.Field{{Key: "decodeOutput", Type: "select", Label: "Decoder output format", LocalizedLabels: map[string]string{"zh-CN": "解码输出格式"},
			Description:           "PCM is carried in WAV with its source channel layout, then converted to the DSP float32 format. This does not select the playback encoding.",
			LocalizedDescriptions: map[string]string{"zh-CN": "PCM 使用 WAV 携带原声道布局，再适配为 DSP 的浮点格式。此项不改变最终播放编码。"},
			DefaultValue:          "native", Options: []pluginsettings.Option{
				{Value: "native", Label: "Original audio", LocalizedLabels: map[string]string{"zh-CN": "原始音频"}},
				{Value: DecodeFloat32, Label: "32-bit floating-point PCM", LocalizedLabels: map[string]string{"zh-CN": "32 位浮点 PCM"}},
				{Value: DecodeSigned16, Label: "16-bit signed PCM", LocalizedLabels: map[string]string{"zh-CN": "16 位整数 PCM"}},
			}}}, Actions: []pluginsettings.Action{},
	}
}

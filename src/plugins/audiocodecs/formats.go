// Package audiocodecs 组合已安装编解码插件的识别能力，不包含任何文件格式规则。
package audiocodecs

import (
	"io"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

var formats = newDefaultRegistry()

func newDefaultRegistry() *audioformat.Registry {
	registry, err := audioformat.New(ncmdecoder.FormatDetector(), audioffmpeg.FormatDetector())
	if err != nil {
		panic(err)
	}
	return registry
}

// DefaultRegistry 共享官方插件声明的只读注册表，包括未启用解码实例的格式识别。
// 识别成功不代表解码实例已启用；播放和分析仍须经过实例生命周期授权。
func DefaultRegistry() *audioformat.Registry { return formats }

func Detect(input io.ReaderAt, size int64) (audioformat.Match, bool, error) {
	return formats.Detect(input, size)
}

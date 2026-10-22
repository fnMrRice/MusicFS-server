package audiocodecs

import (
	"context"
	"io"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

// OpenNative 按插件优先级逐层解除封装，保留可直接消费的原生音频表示。
// 插件自行决定输出是否继续解码；宿主只装配能力，不判断文件头或格式名称。
// 返回值只释放插件创建的中间输出，不拥有调用者传入的源句柄。
func OpenNative(ctx context.Context, decoder *ncmdecoder.Processor, task, asset string, input io.ReaderAt, size int64) (*audioformat.Decoded, error) {
	return Decode(ctx, nil, decoder, audioformat.Request{TaskID: task, AssetID: asset, OutputFormat: "native", MaxOutputBytes: size}, audioformat.Input{Reader: input, Size: size})
}

// Decode 装配实例能力；每轮选择、优先级和终止条件由通用链契约执行。
func Decode(ctx context.Context, audio *audioffmpeg.Processor, decoder *ncmdecoder.Processor, request audioformat.Request, input audioformat.Input) (*audioformat.Decoded, error) {
	chain, err := audioformat.NewChain(decoder.AudioDecoder(), audio.AudioDecoder())
	if err != nil {
		return nil, err
	}
	return chain.Decode(ctx, request, input)
}

package audiocodecs

import (
	"context"
	"io"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
)

// OpenNative 按插件优先级逐层解除封装，保留可直接消费的原生音频表示。
// 插件自行决定输出是否继续解码；宿主只装配能力，不判断文件头或格式名称。
// 返回值只释放插件创建的中间输出，不拥有调用者传入的源句柄。
func OpenNative(ctx context.Context, decoder audioruntime.Decoder, task, asset string, input io.ReaderAt, size int64) (*audioformat.Decoded, error) {
	return Decode(ctx, nil, decoder, audioformat.Request{TaskID: task, AssetID: asset, OutputFormat: "native", MaxOutputBytes: size}, audioformat.Input{Reader: input, Size: size})
}

// Decode 装配实例能力；每轮选择、优先级和终止条件由通用链契约执行。
func Decode(ctx context.Context, audio audioruntime.Decoder, decoder audioruntime.Decoder, request audioformat.Request, input audioformat.Input) (*audioformat.Decoded, error) {
	return DecodeWith(ctx, []audioruntime.Decoder{audio, decoder}, request, input)
}

// DecodeWith 使用已注入实例覆盖插件的无实例能力，支持任意数量的解码层。
func DecodeWith(ctx context.Context, instances []audioruntime.Decoder, request audioformat.Request, input audioformat.Input) (*audioformat.Decoded, error) {
	return DecodeAdmitted(ctx, instances, request, input, nil)
}

// Admission 在真正选择一个解码层后绑定该实例，避免按文件封装猜测依赖。
type Admission func(context.Context, audioruntime.Decoder, audioformat.Request, audioformat.Input) (context.Context, error)

func DecodeAdmitted(ctx context.Context, instances []audioruntime.Decoder, request audioformat.Request, input audioformat.Input, admit Admission) (*audioformat.Decoded, error) {
	decoders := registeredDecoders()
	for _, instance := range instances {
		if instance == nil {
			continue
		}
		decoder := instance.AudioDecoder()
		if decoder.PluginID == "" {
			continue
		}
		declared := decoder
		decoder.Decode = func(ctx context.Context, request audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
			return decodeInstance(ctx, instance, declared, request, input, admit)
		}
		decoders[decoder.PluginID] = decoder
	}
	entries := make([]audioformat.Decoder, 0, len(decoders))
	for _, decoder := range decoders {
		entries = append(entries, decoder)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].PluginID < entries[j].PluginID })
	chain, err := audioformat.NewChain(entries...)
	if err != nil {
		return nil, err
	}
	return chain.Decode(ctx, request, input)
}

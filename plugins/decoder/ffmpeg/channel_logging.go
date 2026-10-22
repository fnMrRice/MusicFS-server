package audioffmpeg

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/channelmix"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// 转换插件日志留在宿主边界，只记录声道数和处理数量。
type loggedChannelConverter struct {
	converter     *channelmix.Converter
	ctx           context.Context
	inputChannels int
}

func newLoggedChannelConverter(source, target audio.Format) (*loggedChannelConverter, error) {
	ctx := pluginlog.Context(context.Background(), channelmix.PluginID, "")
	finish := pluginlog.Operation(ctx, "channelmix.create")
	converter, err := channelmix.New(source, target)
	finish(err)
	if err != nil {
		return nil, err
	}
	pluginlog.Logger(ctx).DebugContext(ctx, "Channel converter ready", "inputChannels", source.Channels, "outputChannels", target.Channels)
	return &loggedChannelConverter{converter: converter, ctx: ctx, inputChannels: source.Channels}, nil
}

func (c *loggedChannelConverter) Process(samples []float32) ([]float32, error) {
	logger := pluginlog.Logger(c.ctx)
	started := time.Now()
	logger.Log(c.ctx, pluginlog.LevelTrace, "Channel conversion started", "sampleCount", len(samples))
	converted, err := c.converter.Process(samples)
	if err != nil {
		logger.WarnContext(c.ctx, "Channel conversion failed", "errorCode", pluginlog.ErrorCode(err))
		return nil, err
	}
	logger.Log(c.ctx, pluginlog.LevelTrace, "Channel conversion completed", "frames", len(samples)/c.inputChannels, "inputSamples", len(samples), "outputSamples", len(converted), "durationUs", time.Since(started).Microseconds())
	return converted, nil
}

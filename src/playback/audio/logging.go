package audio

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

// 在稳定的节点边界记录官方效果调用，不让日志代码进入音效算法。
type loggedNode struct {
	Node
	ctx context.Context
}

func createLoggedNode(name string, factory Factory, params map[string]float64) (Node, error) {
	id := name
	if descriptor, ok := Definition(name); ok && descriptor.PluginID != "" {
		id = descriptor.PluginID
	}
	ctx := pluginlog.Context(context.Background(), id, "")
	finish := pluginlog.Operation(ctx, "dsp.create")
	node, err := factory(params)
	finish(err)
	if err != nil {
		return nil, err
	}
	return &loggedNode{Node: node, ctx: ctx}, nil
}

func (n *loggedNode) Configure(format Format) (err error) {
	finish := pluginlog.Operation(n.ctx, "dsp.configure")
	defer func() { finish(err) }()
	return n.Node.Configure(format)
}

func (n *loggedNode) DescribeIO(format Format) (Output, error) {
	return DescribeIO(n.Node, format)
}

func (n *loggedNode) ConfigureIO(format Format) (output Output, err error) {
	finish := pluginlog.Operation(n.ctx, "dsp.configure")
	defer func() { finish(err) }()
	return configureIO(n.Node, format)
}

func (n *loggedNode) Process(block *Block) (err error) {
	logger := pluginlog.Logger(n.ctx)
	started := time.Now()
	logger.Log(n.ctx, pluginlog.LevelTrace, "DSP effect block started")
	defer func() {
		if err != nil {
			logger.WarnContext(n.ctx, "DSP effect block failed", "errorCode", pluginlog.ErrorCode(err))
			return
		}
		frames, samples := 0, 0
		if block != nil {
			frames, samples = block.Frames, len(block.Samples)
		}
		logger.Log(n.ctx, pluginlog.LevelTrace, "DSP effect block completed", "frames", frames, "sampleCount", samples, "durationUs", time.Since(started).Microseconds())
	}()
	return n.Node.Process(block)
}

func (n *loggedNode) Reset() (err error) {
	finish := pluginlog.Operation(n.ctx, "dsp.reset")
	defer func() { finish(err) }()
	return n.Node.Reset()
}

func (n *loggedNode) Drain() (err error) {
	finish := pluginlog.Operation(n.ctx, "dsp.drain")
	defer func() { finish(err) }()
	return n.Node.Drain()
}

func (n *loggedNode) Close() (err error) {
	finish := pluginlog.Operation(n.ctx, "dsp.close")
	defer func() { finish(err) }()
	return n.Node.Close()
}

func (n *loggedNode) Stateless() bool {
	stateless, ok := n.Node.(interface{ Stateless() bool })
	return ok && stateless.Stateless()
}

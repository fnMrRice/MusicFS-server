package audio

import (
	"context"
	"fmt"
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
	if node == nil && err == nil {
		err = fmt.Errorf("invalid DSP node")
	}
	finish(err)
	if err != nil {
		return nil, err
	}
	return &loggedNode{Node: node, ctx: ctx}, nil
}

func (n *loggedNode) DescribeIO(format Format) (Output, error) {
	return DescribeIO(n.Node, format)
}

func (n *loggedNode) ConfigureIO(format Format) (output Output, err error) {
	finish := pluginlog.Operation(n.ctx, "dsp.configure")
	defer func() { finish(err) }()
	if err := format.Validate(); err != nil {
		return Output{}, err
	}
	output, err = n.Node.ConfigureIO(format)
	if err != nil {
		return Output{}, err
	}
	return validateOutput(format, output)
}

func (n *loggedNode) Process(block *Block) error {
	logger := pluginlog.Logger(n.ctx)
	trace := logger.Enabled(n.ctx, pluginlog.LevelTrace)
	var started time.Time
	if trace {
		started = time.Now()
		logger.Log(n.ctx, pluginlog.LevelTrace, "DSP effect block started")
	}
	if err := n.Node.Process(block); err != nil {
		logger.WarnContext(n.ctx, "DSP effect block failed", "errorCode", pluginlog.ErrorCode(err))
		return err
	}
	if trace {
		frames, samples := 0, 0
		if block != nil {
			frames, samples = block.Frames, len(block.Samples)
		}
		logger.Log(n.ctx, pluginlog.LevelTrace, "DSP effect block completed", "frames", frames, "sampleCount", samples, "durationUs", time.Since(started).Microseconds())
	}
	return nil
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

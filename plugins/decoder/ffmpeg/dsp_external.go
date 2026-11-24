package audioffmpeg

import (
	"context"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

// 外部进程延迟到实际输入格式已协商后启动，避免无效声道组合触发插件。
type externalDSP struct {
	ctx      context.Context
	config   dspworker.Config
	params   map[string]float64
	versions map[string]string
	process  *dspworker.Process
}

func (*externalDSP) DescribeIO(format audio.Format) (audio.Output, error) {
	if format != audio.Canonical {
		return audio.Output{}, fmt.Errorf("UNSUPPORTED: external DSP v1 requires stereo output and input")
	}
	return audio.Output{Format: format, CanContinueDSP: true}, nil
}

func (n *externalDSP) ConfigureIO(format audio.Format) (audio.Output, error) {
	output, err := n.DescribeIO(format)
	if err != nil {
		return audio.Output{}, err
	}
	n.Close()
	process, err := dspworker.New(n.ctx, n.config, n.params)
	if err != nil {
		return audio.Output{}, err
	}
	if err := process.Configure(format); err != nil {
		process.Close()
		return audio.Output{}, err
	}
	n.process = process
	if n.versions != nil {
		n.versions[n.config.Name] = process.PluginVersion()
	}
	return output, nil
}

func (n *externalDSP) Process(block *audio.Block) error {
	if n.process == nil {
		return fmt.Errorf("PROTOCOL_MISMATCH: external DSP was not configured")
	}
	return n.process.Process(block)
}

func (n *externalDSP) Reset() error {
	if n.process == nil {
		return nil
	}
	return n.process.Reset()
}
func (n *externalDSP) Drain() error {
	if n.process == nil {
		return nil
	}
	return n.process.Drain()
}
func (n *externalDSP) Close() error {
	if n.process == nil {
		return nil
	}
	err := n.process.Close()
	n.process = nil
	return err
}
func (*externalDSP) DelayFrames() int64 { return 0 }
func (*externalDSP) Stateless() bool    { return true }

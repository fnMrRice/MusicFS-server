package channelmix

import (
	"fmt"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// Node 将声道矩阵转换暴露为可排序的 DSP 节点。
type Node struct {
	input     audio.Format
	output    audio.Format
	converter *Converter
}

func NewNode(params map[string]float64) (audio.Node, error) {
	channels := float64(2)
	for name, value := range params {
		if name != "outputChannels" {
			return nil, fmt.Errorf("UNSUPPORTED: unknown channel conversion parameter")
		}
		channels = value
	}
	layout := ""
	switch channels {
	case 1:
		layout = "mono"
	case 2:
		layout = "stereo"
	case 6:
		layout = "5.1"
	case 8:
		layout = "7.1"
	default:
		return nil, fmt.Errorf("UNSUPPORTED: outputChannels must be 1, 2, 6 or 8")
	}
	format, err := audio.FormatForLayout(layout)
	if err != nil {
		return nil, err
	}
	return &Node{output: format}, nil
}

func (n *Node) DescribeIO(input audio.Format) (audio.Output, error) {
	if err := input.Validate(); err != nil {
		return audio.Output{}, err
	}
	return audio.Output{Format: n.output, CanContinueDSP: true}, nil
}

func (n *Node) ConfigureIO(input audio.Format) (audio.Output, error) {
	n.converter = nil
	output, err := n.DescribeIO(input)
	if err != nil {
		return audio.Output{}, err
	}
	converter, err := New(input, output.Format)
	if err != nil {
		return audio.Output{}, err
	}
	n.input, n.converter = input, converter
	return output, nil
}

func (n *Node) Process(block *audio.Block) error {
	if n.converter == nil {
		return fmt.Errorf("DSP channel conversion not configured")
	}
	if block == nil || block.Validate() != nil || block.Format != n.input {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid channel conversion block")
	}
	samples, err := n.converter.Process(block.Samples)
	if err != nil {
		return err
	}
	block.Samples, block.Format = samples, n.output
	return nil
}

func (*Node) Reset() error       { return nil }
func (*Node) Drain() error       { return nil }
func (*Node) DelayFrames() int64 { return 0 }
func (*Node) Stateless() bool    { return true }
func (n *Node) Close() error {
	n.converter = nil
	n.input = audio.Format{}
	return nil
}

func Register(registry *audio.Registry) error {
	for _, name := range []string{"channelmix", "musicfs.dsp.channelmix", PluginID} {
		if err := registry.Register(name, NewNode); err != nil {
			return err
		}
	}
	return nil
}

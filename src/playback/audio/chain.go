package audio

import (
	"fmt"
	"math"
)

const MaxChainNodes = 16

// Chain 逐节点协商声道格式，保持帧数和时间轴不变，不支持额外尾音或前视延迟。
type Chain struct {
	nodes      []Node
	outputs    []Output
	input      Format
	output     Output
	configured bool
}

func NewChain(registry *Registry, specs []NodeSpec) (*Chain, error) {
	if registry == nil {
		return nil, fmt.Errorf("invalid DSP registry")
	}
	if len(specs) > MaxChainNodes {
		return nil, fmt.Errorf("UNSUPPORTED: DSP chain exceeds node limit")
	}
	chain := &Chain{}
	for _, spec := range specs {
		node, err := registry.CreateVersioned(spec)
		if err != nil {
			chain.Close()
			return nil, fmt.Errorf("DSP %s: %w", spec.Name, err)
		}
		chain.nodes = append(chain.nodes, node)
	}
	return chain, nil
}

func (c *Chain) Configure(format Format) error {
	_, err := c.ConfigureIO(format)
	return err
}

func (c *Chain) ConfigureIO(format Format) (Output, error) {
	c.configured = false
	c.input, c.output, c.outputs = Format{}, Output{}, nil
	if err := format.Validate(); err != nil {
		return Output{}, err
	}
	output := Output{Format: format, CanContinueDSP: true}
	outputs := make([]Output, 0, len(c.nodes))
	for index, node := range c.nodes {
		if !output.CanContinueDSP {
			return Output{}, fmt.Errorf("UNSUPPORTED: DSP node %d follows a terminal output", index)
		}
		var err error
		output, err = configureIO(node, output.Format)
		if err != nil {
			return Output{}, fmt.Errorf("DSP node %d: %w", index, err)
		}
		if node.DelayFrames() != 0 {
			return Output{}, fmt.Errorf("UNSUPPORTED: DSP v1 requires zero latency")
		}
		outputs = append(outputs, output)
	}
	c.input, c.output, c.outputs = format, output, outputs
	c.configured = true
	return output, nil
}

func (c *Chain) Process(block *Block) error {
	if !c.configured {
		return fmt.Errorf("DSP chain not configured")
	}
	if block == nil {
		return fmt.Errorf("invalid PCM block")
	}
	if err := block.Validate(); err != nil {
		return err
	}
	if block.Format != c.input {
		return fmt.Errorf("PROTOCOL_MISMATCH: DSP input differs from configured format")
	}
	for index, node := range c.nodes {
		before := *block
		if err := node.Process(block); err != nil {
			return err
		}
		if err := block.Validate(); err != nil {
			return err
		}
		if block.Format != c.outputs[index].Format {
			return fmt.Errorf("PROTOCOL_MISMATCH: DSP output differs from declared format")
		}
		if block.Frames != before.Frames || block.Sequence != before.Sequence || block.PositionFrames != before.PositionFrames || block.End != before.End || block.Discontinuity != before.Discontinuity {
			return fmt.Errorf("PROTOCOL_MISMATCH: DSP changed block timing")
		}
		for _, sample := range block.Samples {
			if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
				return fmt.Errorf("PROTOCOL_MISMATCH: DSP produced non-finite PCM")
			}
		}
	}
	return nil
}

func (c *Chain) Reset() error {
	for _, node := range c.nodes {
		if err := node.Reset(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chain) Drain() error {
	for _, node := range c.nodes {
		if err := node.Drain(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chain) Close() error {
	var first error
	for i := len(c.nodes) - 1; i >= 0; i-- {
		if err := c.nodes[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	c.nodes = nil
	c.configured = false
	c.input, c.output, c.outputs = Format{}, Output{}, nil
	return first
}

func (*Chain) DelayFrames() int64 { return 0 }

// Stateless 仅在所有节点明确声明无状态时允许跳过定位之前的音频。
func (c *Chain) Stateless() bool {
	for _, node := range c.nodes {
		stateless, ok := node.(interface{ Stateless() bool })
		if !ok || !stateless.Stateless() {
			return false
		}
	}
	return true
}

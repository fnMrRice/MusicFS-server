package audio

import (
	"fmt"
	"math"
)

const MaxChainNodes = 16

// Chain 保持帧数、格式和时间轴不变，不支持额外尾音或前视延迟。
type Chain struct{ nodes []Node }

func NewChain(registry *Registry, specs []NodeSpec) (*Chain, error) {
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
	if err := format.Validate(); err != nil {
		return err
	}
	for _, node := range c.nodes {
		if err := node.Configure(format); err != nil {
			return err
		}
		if node.DelayFrames() != 0 {
			return fmt.Errorf("UNSUPPORTED: DSP v1 requires zero latency")
		}
	}
	return nil
}

func (c *Chain) Process(block *Block) error {
	if err := block.Validate(); err != nil {
		return err
	}
	for _, node := range c.nodes {
		before := *block
		if err := node.Process(block); err != nil {
			return err
		}
		if err := block.Validate(); err != nil {
			return err
		}
		if block.Format != before.Format || block.Frames != before.Frames || block.Sequence != before.Sequence || block.PositionFrames != before.PositionFrames || block.End != before.End || block.Discontinuity != before.Discontinuity {
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

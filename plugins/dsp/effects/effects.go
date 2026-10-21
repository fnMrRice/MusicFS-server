// Package effects 提供固定格式、固定帧数的有状态音效。
package effects

import (
	"fmt"
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type parameter struct{ value, minimum, maximum float64 }

func parameters(input map[string]float64, specs map[string]parameter) (map[string]float64, error) {
	values := make(map[string]float64, len(specs))
	for name, spec := range specs {
		values[name] = spec.value
	}
	for name, value := range input {
		spec, ok := specs[name]
		if !ok {
			return nil, fmt.Errorf("UNSUPPORTED: unknown DSP parameter %q", name)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < spec.minimum || value > spec.maximum {
			return nil, fmt.Errorf("UNSUPPORTED: DSP parameter %s must be between %g and %g", name, spec.minimum, spec.maximum)
		}
		values[name] = value
	}
	return values, nil
}

type algorithm interface {
	prepare(audio.Format)
	frame(input []float32, output []float64)
	reset()
	release()
}

type node struct {
	algorithm  algorithm
	configured bool
	format     audio.Format
	output     []float64
}

func (n *node) Configure(format audio.Format) error {
	if err := format.Validate(); err != nil {
		return err
	}
	n.algorithm.prepare(format)
	n.format = format
	n.output = make([]float64, format.Channels)
	n.configured = true
	return nil
}

func (n *node) Process(block *audio.Block) error {
	if !n.configured {
		return fmt.Errorf("DSP not configured")
	}
	if block == nil {
		return fmt.Errorf("invalid PCM block")
	}
	if err := block.Validate(); err != nil {
		return err
	}
	if block.Format != n.format {
		return fmt.Errorf("UNSUPPORTED: PCM block differs from configured format")
	}
	// 先检查整块输入，避免错误样本污染滤波器状态或部分改写输入。
	for _, sample := range block.Samples {
		if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
			return fmt.Errorf("invalid PCM sample")
		}
	}
	if block.Discontinuity {
		n.algorithm.reset()
	}
	for i := 0; i < len(block.Samples); i += n.format.Channels {
		n.algorithm.frame(block.Samples[i:i+n.format.Channels], n.output)
		for channel, sample := range n.output {
			block.Samples[i+channel] = float32(max(-1, min(1, sample)))
		}
	}
	return nil
}

func (n *node) Reset() error { n.algorithm.reset(); return nil }

// Drain 不追加音频；混响只在源音频提供的帧内自然衰减。
func (*node) Drain() error { return nil }

func (n *node) Close() error {
	n.algorithm.release()
	n.configured = false
	n.format = audio.Format{}
	n.output = nil
	return nil
}

func (*node) DelayFrames() int64 { return 0 }
func (*node) Stateless() bool    { return false }

func Register(registry *audio.Registry) error {
	for _, entry := range []struct {
		name    string
		factory audio.Factory
	}{
		{"equalizer", NewEqualizer},
		{"compressor", NewCompressor},
		{"reverb", NewReverb},
	} {
		if err := registry.Register(entry.name, entry.factory); err != nil {
			return err
		}
	}
	return nil
}

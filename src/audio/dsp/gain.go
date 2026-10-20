package dsp

import (
	"fmt"
	"math"

	"github.com/fnMrRice/MusicFS-server/src/audio"
)

type Gain struct {
	multiplier float32
	configured bool
}

func NewGain(db float64) (audio.Node, error) {
	if math.IsNaN(db) || math.IsInf(db, 0) || db < -96 || db > 12 {
		return nil, fmt.Errorf("UNSUPPORTED: gain must be between -96 and 12 dB")
	}
	return &Gain{multiplier: float32(math.Pow(10, db/20))}, nil
}
func (g *Gain) Configure(format audio.Format) error {
	if format != audio.Canonical {
		return fmt.Errorf("UNSUPPORTED: unsupported PCM format")
	}
	g.configured = true
	return nil
}
func (g *Gain) Process(block *audio.Block) error {
	if !g.configured {
		return fmt.Errorf("DSP not configured")
	}
	if err := block.Validate(); err != nil {
		return err
	}
	for i, value := range block.Samples {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("invalid PCM sample")
		}
		v := value * g.multiplier
		block.Samples[i] = max(-1, min(1, v))
	}
	return nil
}
func (g *Gain) Reset() error       { return nil }
func (g *Gain) Drain() error       { return nil }
func (g *Gain) Close() error       { g.configured = false; return nil }
func (g *Gain) DelayFrames() int64 { return 0 }
func (g *Gain) Stateless() bool    { return true }
func Register(r *audio.Registry) error {
	if err := r.Register("gain", func(p map[string]float64) (audio.Node, error) {
		if len(p) != 1 {
			return nil, fmt.Errorf("invalid gain parameters")
		}
		db, ok := p["gainDB"]
		if !ok {
			return nil, fmt.Errorf("missing gainDB")
		}
		return NewGain(db)
	}); err != nil {
		return err
	}
	return r.Register("bypass", func(p map[string]float64) (audio.Node, error) {
		if len(p) != 0 {
			return nil, fmt.Errorf("unexpected bypass parameters")
		}
		return NewGain(0)
	})
}

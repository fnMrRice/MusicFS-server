package dsp

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type Gain struct {
	multiplier float32
	configured bool
	logger     *slog.Logger
}

func (g *Gain) log() *slog.Logger {
	if g.logger != nil {
		return g.logger
	}
	return slog.Default().With("pluginId", "musicfs.dsp.gain")
}

func NewGain(db float64) (audio.Node, error) {
	if math.IsNaN(db) || math.IsInf(db, 0) || db < -96 || db > 12 {
		return nil, fmt.Errorf("UNSUPPORTED: gain must be between -96 and 12 dB")
	}
	logger := slog.Default().With("pluginId", "musicfs.dsp.gain")
	logger.Debug("DSP gain node created")
	return &Gain{multiplier: float32(math.Pow(10, db/20)), logger: logger}, nil
}
func (g *Gain) Configure(format audio.Format) error {
	if format != audio.Canonical {
		return fmt.Errorf("UNSUPPORTED: unsupported PCM format")
	}
	g.configured = true
	g.log().Debug("DSP gain node configured")
	return nil
}
func (g *Gain) Process(block *audio.Block) (processErr error) {
	defer func() {
		if processErr != nil {
			g.log().Warn("DSP gain block failed", "errorCode", pluginlog.ErrorCode(processErr))
		}
	}()
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
	g.log().Log(context.Background(), pluginlog.LevelTrace, "DSP gain block processed", "frames", block.Frames, "sequence", block.Sequence)
	return nil
}
func (g *Gain) Reset() error { return nil }
func (g *Gain) Drain() error { return nil }
func (g *Gain) Close() error {
	g.configured = false
	g.log().Debug("DSP gain node closed")
	return nil
}
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

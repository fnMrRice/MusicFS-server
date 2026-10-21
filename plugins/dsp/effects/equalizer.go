package effects

import (
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type equalizer struct {
	b0, b1, b2, a1, a2 float64
	z1, z2             []float64
}

// NewEqualizer 创建单段参数均衡器；串联多个节点可构成多段均衡器。
func NewEqualizer(input map[string]float64) (audio.Node, error) {
	p, err := parameters(input, map[string]parameter{
		"frequencyHz": {1000, 20, 20000},
		"q":           {0.707, 0.1, 20},
		"gainDB":      {0, -24, 24},
	})
	if err != nil {
		return nil, err
	}
	// W3C Audio EQ Cookbook 的 peakingEQ 系数，归一化后使用转置直接二型。
	// https://www.w3.org/TR/2021/NOTE-audio-eq-cookbook-20210608/
	a := math.Pow(10, p["gainDB"]/40)
	w := 2 * math.Pi * p["frequencyHz"] / float64(audio.Canonical.SampleRate)
	alpha := math.Sin(w) / (2 * p["q"])
	a0 := 1 + alpha/a
	return &node{algorithm: &equalizer{
		b0: (1 + alpha*a) / a0,
		b1: -2 * math.Cos(w) / a0,
		b2: (1 - alpha*a) / a0,
		a1: -2 * math.Cos(w) / a0,
		a2: (1 - alpha/a) / a0,
	}}, nil
}

func (e *equalizer) frame(input []float32, output []float64) {
	for channel, sample := range input {
		output[channel] = e.sample(float64(sample), channel)
	}
}

func (e *equalizer) sample(input float64, channel int) float64 {
	output := e.b0*input + e.z1[channel]
	e.z1[channel] = e.b1*input - e.a1*output + e.z2[channel]
	e.z2[channel] = e.b2*input - e.a2*output
	return output
}

func (e *equalizer) reset() { clear(e.z1); clear(e.z2) }
func (e *equalizer) prepare(format audio.Format) {
	e.z1, e.z2 = make([]float64, format.Channels), make([]float64, format.Channels)
}
func (e *equalizer) release() { e.z1, e.z2 = nil, nil }

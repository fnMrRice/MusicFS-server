package effects

import (
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type compressor struct {
	threshold, slope, knee, makeup float64
	attack, releaseCoefficient     float64
	reduction                      float64
}

// NewCompressor 创建所有声道链接的前馈峰值压缩器，不引入前瞻延迟。
func NewCompressor(input map[string]float64) (audio.Node, error) {
	p, err := parameters(input, map[string]parameter{
		"thresholdDB": {-18, -60, 0},
		"ratio":       {4, 1, 20},
		"attackMs":    {10, 0.1, 200},
		"releaseMs":   {100, 10, 3000},
		"makeupDB":    {0, 0, 24},
		"kneeDB":      {6, 0, 24},
	})
	if err != nil {
		return nil, err
	}
	return &node{algorithm: &compressor{
		threshold: p["thresholdDB"], slope: 1 - 1/p["ratio"],
		knee: p["kneeDB"], makeup: p["makeupDB"],
		attack:             math.Exp(-1000 / (p["attackMs"] * float64(audio.Canonical.SampleRate))),
		releaseCoefficient: math.Exp(-1000 / (p["releaseMs"] * float64(audio.Canonical.SampleRate))),
	}}, nil
}

func (c *compressor) frame(input []float32, output []float64) {
	// 共用所有声道中的最大峰值，避免独立压缩导致声像移动。
	peak := 0.0
	for _, sample := range input {
		peak = max(peak, math.Abs(float64(sample)))
	}
	level := -1000.0
	if peak > 0 {
		level = 20 * math.Log10(peak)
	}
	over := level - c.threshold
	target := 0.0
	// 二次软拐点采用 Giannoulis 等 2012 年论文公式 (4)，配合对数域增益平滑。
	// https://joshreiss.github.io/documents/2012/GiannoulisMassbergReiss-dynamicrangecompression-JAES2012.pdf
	// 时间参数为一阶滤波的时间常数，达到目标变化量的约 63.2%。
	if over >= c.knee/2 {
		target = c.slope * over
	} else if c.knee > 0 && over > -c.knee/2 {
		distance := over + c.knee/2
		target = c.slope * distance * distance / (2 * c.knee)
	}
	coefficient := c.releaseCoefficient
	if target > c.reduction {
		coefficient = c.attack
	}
	c.reduction = coefficient*c.reduction + (1-coefficient)*target
	gain := math.Pow(10, (c.makeup-c.reduction)/20)
	for channel, sample := range input {
		output[channel] = float64(sample) * gain
	}
}

func (c *compressor) reset()               { c.reduction = 0 }
func (c *compressor) prepare(audio.Format) { c.reset() }
func (c *compressor) release()             { c.reset() }

package effects

import "github.com/fnMrRice/MusicFS-server/src/playback/audio"

type delayLine struct {
	samples []float64
	index   int
	lowpass float64
}

func (d *delayLine) write(value float64) {
	d.samples[d.index] = value
	d.index = (d.index + 1) % len(d.samples)
}

type reverb struct {
	feedback, damping, wet float64
	combs                  [][4]delayLine
	allpasses              [][2]delayLine
	lfe                    int
}

// NewReverb 创建轻量 Schroeder 混响，曲末不追加额外帧。
func NewReverb(input map[string]float64) (audio.Node, error) {
	p, err := parameters(input, map[string]parameter{
		"roomSize": {0.5, 0, 1},
		"damping":  {0.5, 0, 1},
		"wet":      {0.2, 0, 1},
	})
	if err != nil {
		return nil, err
	}
	return &node{algorithm: &reverb{
		feedback: 0.7 + 0.28*p["roomSize"], damping: 0.4 * p["damping"], wet: p["wet"],
	}}, nil
}

func (r *reverb) prepare(format audio.Format) {
	// 并联低通反馈梳状后串联全通；各声道采用不同延迟形成空间感。
	// 拓扑参考 STK FreeVerb，但这里使用独立声道、较少滤波器及真全通。
	// https://github.com/thestk/stk/blob/master/src/FreeVerb.cpp
	r.combs = make([][4]delayLine, format.Channels)
	r.allpasses = make([][2]delayLine, format.Channels)
	r.lfe = -1
	if format.Layout == "5.1" || format.Layout == "7.1" {
		r.lfe = 3
	}
	for channel := range format.Channels {
		if channel == r.lfe {
			continue
		}
		for i, frames := range [...]int{1423, 1559, 1613, 1789} {
			r.combs[channel][i] = delayLine{samples: make([]float64, frames+23*channel)}
		}
		for i, frames := range [...]int{225, 443} {
			r.allpasses[channel][i] = delayLine{samples: make([]float64, frames+23*channel)}
		}
	}
}

func (r *reverb) frame(input []float32, output []float64) {
	for channel, sample := range input {
		// LFE 保持原始干声，不增加混响或随湿度衰减低频效果声道。
		if channel == r.lfe {
			output[channel] = float64(sample)
			continue
		}
		output[channel] = r.sample(float64(sample), channel)
	}
}

func (r *reverb) sample(input float64, channel int) float64 {
	sum := 0.0
	// 按反馈量归一化激励，避免大房间参数使直流增益无限增大。
	excitation := input * (1 - r.feedback) / float64(len(r.combs[channel]))
	for i := range r.combs[channel] {
		delay := &r.combs[channel][i]
		output := delay.samples[delay.index]
		delay.lowpass = output*(1-r.damping) + delay.lowpass*r.damping
		delay.write(excitation + delay.lowpass*r.feedback)
		sum += output
	}
	for i := range r.allpasses[channel] {
		delay := &r.allpasses[channel][i]
		output := delay.samples[delay.index] - 0.5*sum
		delay.write(sum + 0.5*output)
		sum = output
	}
	return input*(1-r.wet) + sum*r.wet
}

func (r *reverb) reset() {
	for channel := range r.combs {
		for i := range r.combs[channel] {
			delay := &r.combs[channel][i]
			clear(delay.samples)
			delay.index, delay.lowpass = 0, 0
		}
		for i := range r.allpasses[channel] {
			delay := &r.allpasses[channel][i]
			clear(delay.samples)
			delay.index, delay.lowpass = 0, 0
		}
	}
}

func (r *reverb) release() {
	r.combs = nil
	r.allpasses = nil
	r.lfe = -1
}

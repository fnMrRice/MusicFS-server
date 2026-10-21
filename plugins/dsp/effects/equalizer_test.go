package effects_test

import (
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
)

func TestEqualizerFrequencyResponseAndChannelIsolation(t *testing.T) {
	for _, gain := range []float64{-12, 0, 12} {
		node := configured(t, effects.NewEqualizer, map[string]float64{"frequencyHz": 1000, "q": 2, "gainDB": gain})
		input := make([]float32, 48000*2)
		for i := range 48000 {
			input[i*2] = float32(0.05 * math.Sin(2*math.Pi*1000*float64(i)/48000))
		}
		output := render(t, node, input, 1024, false)
		inputEnergy, outputEnergy := 0.0, 0.0
		for i := 24000; i < 48000; i++ {
			inputEnergy += float64(input[i*2]) * float64(input[i*2])
			outputEnergy += float64(output[i*2]) * float64(output[i*2])
			if output[i*2+1] != 0 {
				t.Fatal("equalizer leaked between channels")
			}
		}
		measured := 20 * math.Log10(math.Sqrt(outputEnergy/inputEnergy))
		if math.Abs(measured-gain) > 0.001 {
			t.Fatalf("gain %g measured %g", gain, measured)
		}
	}
	// 中心频率外的直流分量在稳态下保持单位增益。
	node := configured(t, effects.NewEqualizer, map[string]float64{"gainDB": 24, "q": 0.1})
	input := make([]float32, 48000*2)
	for i := range input {
		input[i] = 0.01
	}
	output := render(t, node, input, 4096, false)
	if math.Abs(float64(output[len(output)-1])-0.01) > 1e-6 {
		t.Fatal("equalizer changed steady DC gain")
	}
}

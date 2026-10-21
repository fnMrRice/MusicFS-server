package effects_test

import (
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
)

func constant(frames int, left, right float32) []float32 {
	values := make([]float32, frames*2)
	for i := range frames {
		values[2*i], values[2*i+1] = left, right
	}
	return values
}

func TestCompressorStaticCurveStereoLinkAndMakeup(t *testing.T) {
	for _, scenario := range []struct {
		input, knee, makeup, reduction float64
	}{
		{0.01, 0, 0, 0},
		{1, 0, 0, 15},
		{0.1, 8, 0, 0.75},
		{0.01, 0, 6, 0},
	} {
		node := configured(t, effects.NewCompressor, map[string]float64{"thresholdDB": -20, "ratio": 4, "attackMs": 0.1, "kneeDB": scenario.knee, "makeupDB": scenario.makeup})
		output := render(t, node, constant(24000, float32(scenario.input), float32(scenario.input/4)), 777, false)
		left, right := float64(output[len(output)-2]), float64(output[len(output)-1])
		expected := scenario.input * math.Pow(10, (scenario.makeup-scenario.reduction)/20)
		if math.Abs(left-expected) > 1e-6 {
			t.Fatalf("curve output %g, expected %g", left, expected)
		}
		if math.Abs(left/right-4) > 1e-6 {
			t.Fatal("compression changed stereo balance")
		}
	}
	node := configured(t, effects.NewCompressor, map[string]float64{"ratio": 1})
	input := signal(2000)
	output := render(t, node, input, 4096, false)
	for i := range input {
		if input[i] != output[i] {
			t.Fatal("ratio 1 should preserve input")
		}
	}
	linked := configured(t, effects.NewCompressor, map[string]float64{"thresholdDB": -20, "ratio": 4, "attackMs": 0.1, "kneeDB": 0})
	linkedOutput := render(t, linked, constant(2400, 0.25, -1), 4096, false)
	want := -math.Pow(10, -15.0/20)
	if math.Abs(float64(linkedOutput[len(linkedOutput)-1])-want) > 1e-6 ||
		math.Abs(float64(linkedOutput[len(linkedOutput)-2])+want/4) > 1e-6 {
		t.Fatal("compressor did not link the louder negative right channel")
	}
}

func TestCompressorAttackAndReleaseTimeConstants(t *testing.T) {
	node := configured(t, effects.NewCompressor, map[string]float64{"thresholdDB": -20, "ratio": 4, "attackMs": 10, "releaseMs": 100, "kneeDB": 0})
	attack := render(t, node, constant(480, 1, 1), 128, false)
	expected := math.Pow(10, -15*(1-math.Exp(-1))/20)
	if math.Abs(float64(attack[len(attack)-1])-expected) > 1e-6 {
		t.Fatal("incorrect attack time constant")
	}
	if attack[0] <= attack[len(attack)-1] || attack[0] >= 1 {
		t.Fatal("attack did not progress smoothly")
	}
	render(t, node, constant(24000, 1, 1), 4096, false)
	release := render(t, node, constant(4800, 0.01, 0.01), 128, false)
	expected = 0.01 * math.Pow(10, -15*math.Exp(-1)/20)
	if math.Abs(float64(release[len(release)-1])-expected) > 1e-7 {
		t.Fatal("incorrect release time constant")
	}
	if release[0] >= release[len(release)-1] {
		t.Fatal("release did not restore gain")
	}
}

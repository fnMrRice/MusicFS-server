package effects_test

import (
	"math"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
)

func energy(values []float32, startFrame, endFrame int) float64 {
	result := 0.0
	for i := startFrame * 2; i < endFrame*2; i++ {
		result += float64(values[i]) * float64(values[i])
	}
	return result
}

func TestReverbImpulseDecayAndWetMix(t *testing.T) {
	impulse := make([]float32, 144000*2)
	impulse[0], impulse[1] = 0.5, 0.5
	node := configured(t, effects.NewReverb, map[string]float64{"wet": 0.4})
	output := render(t, node, impulse, 1024, false)
	if math.Abs(float64(output[0])-0.3) > 1e-6 {
		t.Fatal("incorrect dry mix")
	}
	early := energy(output, 1400, 24000)
	late := energy(output, 96000, 120000)
	if early <= 0 || late >= early*0.01 {
		t.Fatalf("reverb does not decay: early=%g late=%g", early, late)
	}
	stereoDiffers := false
	for i := 2; i < len(output); i += 2 {
		stereoDiffers = stereoDiffers || output[i] != output[i+1]
	}
	if !stereoDiffers {
		t.Fatal("reverb delay lines are not decorrelated")
	}
	dry := configured(t, effects.NewReverb, map[string]float64{"wet": 0})
	if !slices.Equal(render(t, dry, impulse, 1024, false), impulse) {
		t.Fatal("wet 0 changed dry input")
	}
	if err := node.Drain(); err != nil {
		t.Fatal(err)
	}
}

func TestReverbRoomSizeControlsDecay(t *testing.T) {
	impulse := make([]float32, 144000*2)
	impulse[0], impulse[1] = 0.5, 0.5
	ratios := make([]float64, 2)
	for i, room := range []float64{0, 1} {
		node := configured(t, effects.NewReverb, map[string]float64{"roomSize": room, "wet": 1})
		output := render(t, node, impulse, 4096, false)
		ratios[i] = energy(output, 96000, 120000) / energy(output, 1400, 24000)
	}
	if ratios[1] < ratios[0]*100 {
		t.Fatal("larger room did not extend decay")
	}
}

func TestReverbDampingReducesLateHighFrequencyEnergy(t *testing.T) {
	input := make([]float32, 96000*2)
	for i := range 24000 {
		input[2*i] = float32(0.1 * math.Sin(2*math.Pi*12000*float64(i)/48000))
	}
	var energies [2]float64
	for i, damping := range []float64{0, 1} {
		node := configured(t, effects.NewReverb, map[string]float64{"roomSize": 1, "damping": damping, "wet": 1})
		output := render(t, node, input, 4096, false)
		energies[i] = energy(output, 48000, 96000)
	}
	if energies[1] >= energies[0]*0.1 {
		t.Fatalf("damping did not attenuate high-frequency tail: %v", energies)
	}
}

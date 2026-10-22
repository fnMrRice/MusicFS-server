package channelmix

import (
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func sourceFormat(layout string) audio.Format {
	return audio.Format{SampleFormat: "f32le", SampleRate: 48000, Channels: len(layouts[layout]), Layout: layout}
}

func TestChannelImpulsesRetainSpeakerIdentity(t *testing.T) {
	for _, tc := range []struct {
		input, output string
		destinations  []int
		gains         []float32
	}{
		{"stereo", "stereo", []int{0, 1}, nil},
		{"stereo", "5.1", []int{0, 1}, nil},
		{"stereo", "7.1", []int{0, 1}, nil},
		{"mono", "5.1", []int{2}, nil},
		{"mono", "7.1", []int{2}, nil},
		{"5.1", "5.1", []int{0, 1, 2, 3, 4, 5}, nil},
		{"5.1", "7.1", []int{0, 1, 2, 3, 4, 5}, nil},
		{"5.1(side)", "5.1", []int{0, 1, 2, 3, 4, 5}, nil},
		{"5.1(side)", "7.1", []int{0, 1, 2, 3, 6, 7}, nil},
		{"7.1", "7.1", []int{0, 1, 2, 3, 4, 5, 6, 7}, nil},
		{"7.1", "5.1", []int{0, 1, 2, 3, 4, 5, 4, 5}, []float32{1, 1, 1, 1, 0.5, 0.5, 0.5, 0.5}},
	} {
		t.Run(tc.input+" to "+tc.output, func(t *testing.T) {
			source, target := sourceFormat(tc.input), sourceFormat(tc.output)
			converter, err := New(source, target)
			if err != nil {
				t.Fatal(err)
			}
			for channel := range source.Channels {
				impulse := make([]float32, source.Channels)
				impulse[channel] = 1
				got, err := converter.Process(impulse)
				if err != nil {
					t.Fatal(err)
				}
				for out, actual := range got {
					want := float32(0)
					if out == tc.destinations[channel] {
						want = 1
						if tc.gains != nil {
							want = tc.gains[channel]
						}
					}
					if actual != want {
						t.Fatalf("input channel=%d output channel=%d got=%g want=%g", channel, out, actual, want)
					}
				}
			}
		})
	}
}

func TestStereoDownmixUsesNormalizedCenterAndSurround(t *testing.T) {
	w := math.Sqrt(0.5)
	for _, layout := range []string{"5.1", "5.1(side)", "7.1"} {
		t.Run(layout, func(t *testing.T) {
			source := sourceFormat(layout)
			converter, err := New(source, audio.Canonical)
			if err != nil {
				t.Fatal(err)
			}
			denominator := 1 + 2*w
			if layout == "7.1" {
				denominator += w
			}
			want := [][2]float64{{1 / denominator, 0}, {0, 1 / denominator}, {w / denominator, w / denominator}, {0, 0}, {w / denominator, 0}, {0, w / denominator}, {w / denominator, 0}, {0, w / denominator}}
			for channel := range source.Channels {
				input := make([]float32, source.Channels)
				input[channel] = 1
				got, err := converter.Process(input)
				if err != nil {
					t.Fatal(err)
				}
				for out := range 2 {
					if math.Abs(float64(got[out])-want[channel][out]) > 1e-7 {
						t.Fatalf("channel=%d got=%v want=%v", channel, got, want[channel])
					}
				}
			}
		})
	}
}

func TestMonoCopyAndConversionDoNotChangeFrameCount(t *testing.T) {
	converter, err := New(sourceFormat("mono"), audio.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	got, err := converter.Process([]float32{0.5, -0.25, 0})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.5, 0.5, -0.25, -0.25, 0, 0}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestAllMatricesBoundFullScaleAndRejectInvalidPCM(t *testing.T) {
	for input := range layouts {
		for _, output := range []string{"mono", "stereo", "5.1", "7.1"} {
			converter, err := New(sourceFormat(input), sourceFormat(output))
			if err != nil {
				t.Fatal(err)
			}
			full := make([]float32, len(layouts[input]))
			for i := range full {
				full[i] = 1
			}
			got, err := converter.Process(full)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range got {
				if value < 0 || value > 1 {
					t.Fatalf("%s to %s clips: %v", input, output, got)
				}
			}
			full[0] = float32(math.NaN())
			if _, err := converter.Process(full); err == nil {
				t.Fatal("non-finite input accepted")
			}
			if len(full) > 1 {
				if _, err := converter.Process(full[:len(full)-1]); err == nil {
					t.Fatal("partial frame accepted")
				}
			}
		}
	}
	if _, err := New(sourceFormat("unknown"), audio.Canonical); err == nil {
		t.Fatal("unknown input accepted")
	}
	if _, err := New(audio.Canonical, sourceFormat("quad")); err == nil {
		t.Fatal("unsupported output accepted")
	}
}

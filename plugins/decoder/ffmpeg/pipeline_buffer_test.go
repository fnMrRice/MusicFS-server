package audioffmpeg

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestPipelineBufferSizeAndIntermediateLayoutsPreserveStatefulOutput(t *testing.T) {
	request := pipelineFixture(t)
	request.Plan.Profile = "opus-7.1"
	request.Plan.PositionMs = 733
	request.DSPChain = []audio.NodeSpec{
		{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 2}},
		{Name: "equalizer", Version: 1, Params: map[string]float64{"gainDB": 4}},
		{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 6}},
		{Name: "compressor", Version: 1},
		{Name: "reverb", Version: 1},
		{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}},
	}
	wave := testWAV(8, 0x63f, speakerBursts(8))
	var reference []float32
	for _, size := range []int{64, 257, 4096, 16384} {
		request.BufferFrames = size
		encoded := runWavePipeline(t, request, wave)
		format, samples := decodedOutputPCM(t, request.FFmpegPath, encoded)
		if format.Layout != "7.1" {
			t.Fatal("negotiated pipeline lost selected output layout", format)
		}
		if reference == nil {
			reference = samples
			continue
		}
		if len(reference) != len(samples) {
			t.Fatal("buffer size changed encoded sample count", size)
		}
		for index, sample := range samples {
			if sample != reference[index] {
				t.Fatal("buffer size changed stateful audio output", size, index)
			}
		}
	}
}

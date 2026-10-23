package playback_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type independentRewrite struct {
	audioruntime.Unavailable
	gate *plugincontrol.Gate
}

func (p *independentRewrite) AcquireLease(ctx context.Context) (*plugincontrol.Lease, error) {
	return p.gate.Acquire(ctx)
}
func (p *independentRewrite) AudioDecoder() audioformat.Decoder {
	return audioformat.Decoder{Detector: audioformat.Detector{PluginID: "test.independent.rewrite", Priority: 1000, Extensions: []string{".wav"}, Probe: func(header []byte, _ int64) (audioformat.Match, bool) {
		return audioformat.Match{Format: "wav", MIME: "audio/wav"}, len(header) >= 12 && string(header[:4]) == "RIFF"
	}}, Decode: func(ctx context.Context, _ audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
		data, err := io.ReadAll(io.NewSectionReader(input.Reader, 0, input.Size))
		if err != nil {
			return audioformat.Output{}, err
		}
		data[len(data)-1] ^= 1
		return audioformat.Output{Input: audioformat.Input{Reader: bytes.NewReader(data), Size: int64(len(data)), Format: "wav", MIME: "audio/wav"}, Processing: []audioformat.Processing{{Type: "vendor.losslessRewrite", Version: "2"}}}, ctx.Err()
	}}
}

func TestIndependentDecoderAdmissionAndPublicProcessingDescription(t *testing.T) {
	processor := &independentPCM{}
	f := recoverySetup(t, processor, 0)
	decoder := &independentRewrite{gate: plugincontrol.NewGate(true)}
	f.player.UpdateAudio(processor, decoder)
	description := f.create(t, "independent-wrapper")
	raw, err := json.Marshal(description)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "Playback", raw)
	if len(description.EffectiveProcessing) != 1 || description.EffectiveProcessing[0].Type != "vendor.losslessRewrite" || decoder.gate.Active() != 1 {
		t.Fatalf("selected decoder identity or lease was lost: %+v, leases=%d", description.EffectiveProcessing, decoder.gate.Active())
	}
	actual, err := processedBytes(t, f, description)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	want[len(want)-1] ^= 1
	if !bytes.Equal(actual, want) {
		t.Fatal("injected decoder output was bypassed")
	}
	f.player.Close()
	if decoder.gate.Active() != 0 {
		t.Fatal("decoder lease survived playback shutdown")
	}
	decoder.gate.SetAccepting(false, false)
}

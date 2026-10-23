package playback_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

// 独立 PCM 实现直接传递 WAV 字节，不调用官方解码器或任何外部工具。
type independentPCM struct {
	audioruntime.Unavailable
	processed atomic.Int64
}

var independentProfile = audiotypes.Profile{ID: "test-independent-pcm", MIME: "audio/wav", Codec: "pcm_s16le", Container: "wav", SampleRate: 48000, Channels: 2, Layout: "stereo", SeekMode: "timeRestart"}

func init() {
	if err := audioruntime.RegisterProfiles("test.independent.pcm", func(id string) (audiotypes.Profile, bool) { return independentProfile, id == independentProfile.ID }); err != nil {
		panic(err)
	}
}
func (p *independentPCM) Available() bool { return true }
func (p *independentPCM) RuntimeCapabilities() audioruntime.Capabilities {
	return audioruntime.Capabilities{Version: "1.0.0", DecoderSHA256: strings.Repeat("d", 64), Profiles: []audiotypes.Profile{independentProfile}}
}
func (p *independentPCM) LookupProfile(id string) (audiotypes.Profile, bool) {
	return independentProfile, id == independentProfile.ID
}
func (p *independentPCM) OutputProfile() (audiotypes.Profile, error) { return independentProfile, nil }
func (p *independentPCM) SupportsProfile(profile audiotypes.Profile) bool {
	return profile == independentProfile
}
func (p *independentPCM) SupportsDecodeOutput(string) error                         { return nil }
func (p *independentPCM) SelectPolicy(bool, bool) audioruntime.Processor            { return p }
func (p *independentPCM) SelectSourceChannels(int, bool) audioruntime.Processor     { return p }
func (p *independentPCM) SelectSourceBitrate(int64) (audioruntime.Processor, error) { return p, nil }
func (p *independentPCM) SelectSettings(context.Context, dspsettings.Options) (audioruntime.Processor, error) {
	return p, nil
}
func (p *independentPCM) ValidateSettings(context.Context, dspsettings.Options) error { return nil }
func (p *independentPCM) AvailableOutputLayouts() []string                            { return []string{"stereo"} }
func (p *independentPCM) ChannelMixDisabled() bool                                    { return true }
func (p *independentPCM) EffectsDisabled() bool                                       { return true }
func (p *independentPCM) RuntimeIdentity(string) audiotypes.ProcessingIdentity {
	return audiotypes.ProcessingIdentity{WorkerSHA256: strings.Repeat("c", 64), DecoderSHA256: strings.Repeat("d", 64), PluginVersion: "1.0.0", PipelineRevision: "dsp-v1:" + strings.Repeat("e", 64)}
}
func (p *independentPCM) DeploymentIdentityValue() audiotypes.ProcessingIdentity {
	return p.RuntimeIdentity("")
}
func (p *independentPCM) ProbeOutputMetadata(context.Context, io.ReadSeeker, int64) (audiotypes.Metadata, error) {
	rate := int64(48000)
	return audiotypes.Metadata{Channels: 2, Layout: "stereo", SampleRateHz: &rate}, nil
}
func (p *independentPCM) AudioDecoder() audioformat.Decoder {
	return audioformat.Decoder{Detector: audioformat.Detector{PluginID: "test.independent.pcm", Priority: 900, Extensions: []string{".wav"}, Probe: func(header []byte, size int64) (audioformat.Match, bool) {
		return audioformat.Match{Format: "wav", MIME: "audio/wav"}, len(header) >= 12 && string(header[:4]) == "RIFF"
	}}, Decode: func(ctx context.Context, request audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
		input.Format, input.MIME = "wav", "audio/wav"
		return audioformat.Output{Input: input}, ctx.Err()
	}}
}
func (p *independentPCM) Process(ctx context.Context, plan audiotypes.Plan, input io.ReadSeeker, size int64, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.processed.Add(1)
	_, err := io.CopyN(output, input, size)
	return err
}

func TestIndependentProcessorControlsPlaybackCacheAndRecovery(t *testing.T) {
	processor := &independentPCM{}
	f := recoverySetup(t, processor, 0)
	original, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	first := f.create(t, "independent-first")
	if first.OutputProfile.ID != independentProfile.ID {
		t.Fatalf("wrong processing profile: %+v", first.OutputProfile)
	}
	got, err := processedBytes(t, f, first)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("independent output changed: %v", err)
	}
	second := f.create(t, "independent-second")
	got, err = processedBytes(t, f, second)
	if err != nil || !bytes.Equal(got, original) || processor.processed.Load() != 1 {
		t.Fatalf("independent cache was not reused: %v, calls=%d", err, processor.processed.Load())
	}
	profile, err := f.player.RecoveryProfile()
	if err != nil || profile.OutputProfile != independentProfile.ID || profile.DecoderSHA256 != strings.Repeat("d", 64) {
		t.Fatalf("independent recovery identity rejected: %+v %v", profile, err)
	}
	if !f.player.DSPSettingsConfig().Available {
		t.Fatal("DSP settings incorrectly require an official processor")
	}
	evidence := f.evidence(t, 1)
	result, err := f.player.RecoverSource(t.Context(), f.who, first.SessionID, recoveryRequest(first, f.tracks[1].ID, &evidence), "independent-recovery")
	if err != nil || result.Decision != "continue" {
		t.Fatalf("independent recovery rejected: %+v %v", result, err)
	}
}

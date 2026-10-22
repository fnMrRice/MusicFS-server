package playback_test

import (
	"bytes"
	"os"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

func TestGlobalDSPIsInheritedAndPersonalSettingsReplaceTheChain(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual global DSP validation")
	}
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	f := recoverySetup(t, processor, 0)
	service := dspsettings.New(f.repo, f.player.DSPSettingsConfig)
	global, err := service.ReadGlobal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	options := global.Settings
	options.BufferFrames = 256
	options.Chain = []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -6}}}
	global, err = service.UpdateGlobal(t.Context(), dspsettings.Request{ExpectedRevision: &global.Revision, SchemaRevision: global.SchemaRevision, Settings: &options}, "dsp-global-initial")
	if err != nil {
		t.Fatal(err)
	}
	personal, err := service.Read(t.Context(), f.who.User.ID)
	if err != nil || !personal.UsesGlobal || personal.Settings.BufferFrames != 256 {
		t.Fatal("user without an override did not inherit global DSP", personal, err)
	}
	inherited := f.create(t, "dsp-global-inherited")
	if len(inherited.EffectiveProcessing) != 3 || inherited.EffectiveProcessing[2].GainDB != -6 {
		t.Fatal("global DSP was not applied to playback", inherited.EffectiveProcessing)
	}
	initialAudio, err := processedBytes(t, f, inherited)
	if err != nil {
		t.Fatal(err)
	}
	options.Chain = []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -12}}}
	options.BufferFrames = 64
	personal, err = service.Update(t.Context(), f.who.User.ID, dspsettings.Request{ExpectedRevision: &personal.Revision, SchemaRevision: personal.SchemaRevision, Settings: &options}, "dsp-personal-override")
	if err != nil {
		t.Fatal(err)
	}
	selected := f.create(t, "dsp-personal-selected")
	if len(selected.EffectiveProcessing) != 3 || selected.EffectiveProcessing[2].GainDB != -12 {
		t.Fatal("personal DSP was appended to global chain instead of replacing it", selected.EffectiveProcessing)
	}
	options.Chain = []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -3}}}
	options.BufferFrames = 8192
	global, err = service.UpdateGlobal(t.Context(), dspsettings.Request{ExpectedRevision: &global.Revision, SchemaRevision: global.SchemaRevision, Settings: &options}, "dsp-global-next")
	if err != nil {
		t.Fatal(err)
	}
	retained, err := processedBytes(t, f, inherited)
	if err != nil || !bytes.Equal(initialAudio, retained) {
		t.Fatal("global update changed already active playback", err)
	}
	stillPersonal := f.create(t, "dsp-personal-unaffected")
	if len(stillPersonal.EffectiveProcessing) != 3 || stillPersonal.EffectiveProcessing[2].GainDB != -12 {
		t.Fatal("global update overrode personal chain", stillPersonal.EffectiveProcessing)
	}
	personal, err = service.Update(t.Context(), f.who.User.ID, dspsettings.Request{ExpectedRevision: &personal.Revision, SchemaRevision: personal.SchemaRevision, InheritGlobal: true}, "dsp-personal-inherit")
	if err != nil || !personal.UsesGlobal || personal.Settings.BufferFrames != 8192 {
		t.Fatal("reset did not select updated global defaults", personal, err)
	}
	reset := f.create(t, "dsp-global-reset-playback")
	if len(reset.EffectiveProcessing) != 3 || reset.EffectiveProcessing[2].GainDB != -3 {
		t.Fatal("reset did not change the next playback", reset.EffectiveProcessing)
	}
	if _, err = processedBytes(t, f, reset); err != nil {
		t.Fatal(err)
	}
}

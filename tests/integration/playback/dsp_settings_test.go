package playback_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

func TestDSPSettingsApplyToNewPlaybackAndFreezeSeekRecoveryAndCache(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual DSP settings validation")
	}
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	f := recoverySetup(t, processor, 0)
	ctx := context.Background()
	service := dspsettings.New(f.repo, f.player.DSPSettingsConfig)
	page, err := service.Read(ctx, f.who.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldProfile, err := f.player.RecoveryProfileForUser(ctx, f.who.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := f.evidence(t, 1)
	original := f.create(t, "dsp-settings-original")
	oldAudio, err := processedBytes(t, f, original)
	if err != nil {
		t.Fatal(err)
	}
	options := page.Settings
	options.BufferFrames = 64
	options.Chain = []audio.NodeSpec{
		{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}},
		{Name: "equalizer", Version: 1, Params: map[string]float64{"gainDB": 6}},
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -12}},
		{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 2}},
	}
	page, err = service.Update(ctx, f.who.User.ID, dspsettings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.SchemaRevision, Settings: &options}, "dsp-settings-user-change")
	if err != nil {
		t.Fatal(err)
	}
	other, err := dspsettings.Load(ctx, f.repo, "another-user")
	if err != nil || other != nil {
		t.Fatal("DSP preference leaked to another user", other, err)
	}
	currentProfile, err := f.player.RecoveryProfileForUser(ctx, f.who.User.ID)
	if err != nil || currentProfile == oldProfile {
		t.Fatal("DSP settings did not change processing identity", err)
	}
	selected := f.create(t, "dsp-settings-selected")
	if len(selected.EffectiveProcessing) != 2+len(options.Chain) {
		t.Fatal("new playback did not use the configured ordered DSP nodes", selected.EffectiveProcessing)
	}
	selectedAudio, err := processedBytes(t, f, selected)
	if err != nil || bytes.Equal(oldAudio, selectedAudio) {
		t.Fatal("new DSP settings did not process actual audio", err)
	}
	retained, err := processedBytes(t, f, original)
	if err != nil || !bytes.Equal(oldAudio, retained) {
		t.Fatal("active playback changed after settings update", err)
	}
	result, err := f.player.RecoverSource(ctx, f.who, original.SessionID, recoveryRequest(original, f.tracks[1].ID, &evidence), "dsp-settings-frozen-recovery")
	if err != nil || result.Decision != "continue" || result.Playback == nil || len(result.Playback.EffectiveProcessing) != 2 {
		t.Fatal("recovery did not preserve original DSP configuration", result, err)
	}
	if _, err = processedBytes(t, f, *result.Playback); err != nil {
		t.Fatal(err)
	}
	options.BufferFrames, options.OutputLayout = 16384, "7.1"
	options.Chain[len(options.Chain)-1].Params["outputChannels"] = 8
	page, err = service.Update(ctx, f.who.User.ID, dspsettings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.SchemaRevision, Settings: &options}, "dsp-settings-surround")
	if err != nil {
		t.Fatal(err)
	}
	seek, err := f.player.Control(ctx, f.who, selected.SessionID, "seek", playback.ControlRequest{AttemptID: selected.AttemptID, PositionMs: 1000}, "dsp-settings-frozen-seek")
	if err != nil || seek.OutputProfile.Layout != "stereo" {
		t.Fatal("seek changed the active output layout", seek, err)
	}
	if _, err = processedBytes(t, f, seek); err != nil {
		t.Fatal(err)
	}
	f.request.Capabilities.Formats = append(f.request.Capabilities.Formats, playback.Format{MIME: `audio/webm; codecs="opus"`, CanPlay: "probably"})
	surround := f.create(t, "dsp-settings-new-surround")
	encoded, err := processedBytes(t, f, surround)
	if err != nil || surround.OutputProfile.Channels != 8 {
		t.Fatal("new surround settings were not used", surround, err)
	}
	assertSurroundEncoded(t, ffprobe, encoded, 8)
	records, err := f.repo.ListProcessedCache(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Identity.PositionMs == 1000 && record.Identity.PipelineRevision != currentProfile.ProcessingRevision {
			t.Fatal("seek reused newly saved DSP settings instead of its creation snapshot")
		}
	}
	reloaded, err := dspsettings.New(f.repo, f.player.DSPSettingsConfig).Read(ctx, f.who.User.ID)
	if err != nil || reloaded.Settings.BufferFrames != 16384 || reloaded.Settings.OutputLayout != "7.1" {
		t.Fatal("DSP settings did not survive service reconstruction", err)
	}
}

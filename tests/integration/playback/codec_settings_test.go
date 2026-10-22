package playback_test

import (
	"context"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestCodecSettingsChangeNewSessionsAndPreserveActiveIdentity(t *testing.T) {
	processor, _ := cacheAudioProcessor(t)
	f := recoverySetup(t, processor, 0)
	ctx := context.Background()
	service := settings.New(f.repo, codecsettings.Providers()...)
	page, err := service.Read(ctx, f.who.User.ID, codecsettings.FFmpegID)
	if err != nil || page.Values["decodeOutput"] != "native" {
		t.Fatal("native decoder output was not the default", page, err)
	}
	original := f.create(t, "codec-native-create")
	before, err := f.player.RecoveryProfileForUser(ctx, f.who.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := f.evidence(t, 1)
	revision := page.Revision
	_, err = service.Update(ctx, f.who.User.ID, codecsettings.FFmpegID, settings.Request{ExpectedRevision: &revision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"decodeOutput": "unknown"}})
	if err == nil {
		t.Fatal("unsupported decoder output setting accepted")
	}
	page, err = service.Update(ctx, f.who.User.ID, codecsettings.FFmpegID, settings.Request{ExpectedRevision: &revision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"decodeOutput": audioffmpeg.DecodeSigned16}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.player.RecoveryProfileForUser(ctx, f.who.User.ID)
	if err != nil || current.ProcessingRevision == before.ProcessingRevision {
		t.Fatal("decoder quantization reused recovery evidence", current, err)
	}
	f.request.Preferences.ForceProcessing = false
	selected := f.create(t, "codec-signed-create")
	if selected.DeliveryMode != "processed" {
		t.Fatal("PCM selection did not enter the decoding pipeline")
	}
	if _, err := processedBytes(t, f, selected); err != nil {
		t.Fatal(err)
	}
	if _, err := processedBytes(t, f, original); err != nil {
		t.Fatal(err)
	}
	records, err := f.repo.ListProcessedCache(ctx)
	if err != nil || len(records) != 2 {
		t.Fatal("decoder cache records missing", len(records), err)
	}
	identities := map[string]bool{}
	for _, record := range records {
		identities[record.Identity.PipelineRevision] = true
	}
	if !identities[before.ProcessingRevision] || !identities[current.ProcessingRevision] {
		t.Fatal("active decoder configuration was overwritten", identities)
	}
	result, err := f.player.RecoverSource(ctx, f.who, original.SessionID, recoveryRequest(original, f.tracks[1].ID, &evidence), "codec-change-recovery")
	if err != nil || result.Decision != "promptRestart" {
		t.Fatal("old decoder evidence continued under new quantization", result, err)
	}
	revision = page.Revision
	if _, err = service.Update(ctx, f.who.User.ID, codecsettings.FFmpegID, settings.Request{ExpectedRevision: &revision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"decodeOutput": audioffmpeg.DecodeFloat32}}); err != nil {
		t.Fatal(err)
	}
	seek, err := f.player.Control(ctx, f.who, selected.SessionID, "seek", playback.ControlRequest{AttemptID: selected.AttemptID, PositionMs: 1000}, "codec-retained-seek")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processedBytes(t, f, seek); err != nil {
		t.Fatal(err)
	}
	records, err = f.repo.ListProcessedCache(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Identity.PositionMs == 1000 && record.Identity.PipelineRevision != current.ProcessingRevision {
			t.Fatal("active session changed decoder output after settings edit")
		}
	}
}

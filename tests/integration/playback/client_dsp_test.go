package playback_test

import (
	"bytes"
	"os"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestClientDSPBypassesStoredEffectsAndGainWithoutServerProcessor(t *testing.T) {
	for _, scope := range []string{"personal", "global"} {
		t.Run(scope, func(t *testing.T) {
			old, repo, who, request, _ := playbackService(t)
			old.Close()
			owner, storageID := who.User.ID, dspsettings.StorageID
			if scope == "global" {
				owner, storageID = dspsettings.GlobalOwner, dspsettings.GlobalStorageID
			}
			options := dspsettings.Options{Chain: []audio.NodeSpec{{Name: "reverb", Version: 1}}, OutputLayout: "7.1", BufferFrames: 64}
			if _, err := repo.PutPluginSettings(t.Context(), owner, storageID, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": options}); err != nil {
				t.Fatal(err)
			}
			player, err := playback.New(t.Context(), repo.Store, repo.library, nil, playback.Config{CacheRoot: t.TempDir(), MaxSessions: 8, MaxCacheBytes: 16 << 20, RequireDSP: true})
			if err != nil {
				t.Fatal(err)
			}
			defer player.Close()
			request.Preferences = playback.Preferences{DSPExecution: "client", GainDB: -30}
			description, err := player.Create(t.Context(), who, request, "client-dsp-original")
			if err != nil || description.DSPExecution != "client" || description.DeliveryMode != "proxy" || len(description.EffectiveProcessing) != 0 {
				t.Fatalf("client DSP unexpectedly required server effects: %+v %v", description, err)
			}
			request.Preferences.ForceProcessing = true
			_, err = player.Create(t.Context(), who, request, "client-dsp-force-conversion")
			recoveryError(t, err, "UNSUPPORTED")
		})
	}
}

func TestClientDSPActualAudioCacheSeekAndRecoveryStayWithoutEffects(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for client DSP isolation validation")
	}
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, MaxConcurrent: 1,
		DSPChain: []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -12}}}})
	if err != nil {
		t.Fatal(err)
	}
	f := recoverySetup(t, processor, 0)
	options := dspsettings.Options{Chain: []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -24}}}, OutputLayout: "stereo", BufferFrames: 4096}
	if _, err := f.repo.PutPluginSettings(t.Context(), f.who.User.ID, dspsettings.StorageID, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": options}); err != nil {
		t.Fatal(err)
	}
	f.request.Preferences.GainDB = -6
	server := f.create(t, "server-effects-audio")
	serverAudio, err := processedBytes(t, f, server)
	if err != nil {
		t.Fatal(err)
	}
	f.request.Preferences.DSPExecution = "client"
	client := f.create(t, "client-effects-audio")
	assertClientProcessing(t, client)
	clientAudio, err := processedBytes(t, f, client)
	if err != nil {
		t.Fatal(err)
	}
	if ratio := pcmRMS(t, ffmpeg, clientAudio) / pcmRMS(t, ffmpeg, serverAudio); ratio < 30 || ratio > 33 {
		t.Fatalf("client audio retained server gain: RMS ratio=%f", ratio)
	}
	cache, err := f.repo.ListProcessedCache(t.Context())
	if err != nil || len(cache) != 2 || cache[0].Identity.PipelineRevision == cache[1].Identity.PipelineRevision {
		t.Fatalf("server and client effect caches were not isolated: %+v %v", cache, err)
	}
	f.request.Preferences.GainDB = -40
	clientAgain := f.create(t, "client-effects-ignored-gain")
	again, err := processedBytes(t, f, clientAgain)
	if err != nil || !bytes.Equal(clientAudio, again) {
		t.Fatalf("ignored client gain changed processed audio: %v", err)
	}
	for _, operation := range []string{"seek", "recover"} {
		client, err = f.player.Control(t.Context(), f.who, client.SessionID, operation, playback.ControlRequest{AttemptID: client.AttemptID, PositionMs: 1000,
			ObservedError: &playback.ObservedError{Code: "NETWORK", Message: "Synthetic interruption"}}, "client-effects-"+operation)
		if err != nil {
			t.Fatal(err)
		}
		assertClientProcessing(t, client)
		if _, err := processedBytes(t, f, client); err != nil {
			t.Fatal(err)
		}
	}
	// 人工证据绑定无效果的处理身份；恢复仍沿用创建时的客户端执行归属。
	profile, err := f.player.RecoveryProfileForUser(t.Context(), f.who.User.ID, "client")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := f.player.AddTimeline(t.Context(), f.who.User.ID, timeline.Request{RecordingID: f.recording,
		From: timelineTarget(f.tracks[0]), To: timelineTarget(f.tracks[1]), Range: timeline.Range{EndMs: 6000}, OffsetMs: 1000, Profile: profile}, "client-audio-alignment")
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := f.player.RecoverSource(t.Context(), f.who, client.SessionID, recoveryRequest(client, f.tracks[1].ID, &evidence), "client-effects-cross-source")
	if err != nil || recovered.Decision != "continue" || recovered.Playback == nil {
		t.Fatalf("client recovery failed: %+v %v", recovered, err)
	}
	assertClientProcessing(t, *recovered.Playback)
	if _, err := processedBytes(t, f, *recovered.Playback); err != nil {
		t.Fatal(err)
	}
}

func assertClientProcessing(t *testing.T, description playback.Description) {
	t.Helper()
	if description.DSPExecution != "client" || len(description.EffectiveProcessing) != 0 {
		t.Fatalf("client session regained server effects: %+v", description)
	}
}

func TestDisabledEffectsDoNotLoadExternalPluginsAndKeepSurroundConversion(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for disabled effects validation")
	}
	p, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, MaxConcurrent: 1,
		DisableEffects: true, OutputLayout: "5.1", DSPChain: []audio.NodeSpec{{Name: "test.missing", Version: 1}},
		DSPPlugins: []dspworker.Config{{Name: "test.missing", Executable: "missing-dsp-worker"}}})
	if err != nil {
		t.Fatal(err)
	}
	f := recoverySetup(t, p, 0)
	options := dspsettings.Options{Chain: []audio.NodeSpec{{Name: "reverb", Version: 1}}, OutputLayout: "7.1", BufferFrames: 4096}
	if _, err := f.repo.PutPluginSettings(t.Context(), dspsettings.GlobalOwner, dspsettings.GlobalStorageID, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": options}); err != nil {
		t.Fatal(err)
	}
	service := dspsettings.New(f.repo, f.player.DSPSettingsConfig)
	page, err := service.Read(t.Context(), f.who.User.ID)
	if err != nil || !page.Available || len(page.Catalog) != 0 || len(page.OutputLayouts) != 3 {
		t.Fatalf("disabled effects blocked format configuration: %+v %v", page, err)
	}
	if _, err := service.Update(t.Context(), f.who.User.ID, dspsettings.Request{ExpectedRevision: &page.Revision,
		SchemaRevision: dspsettings.SchemaRevision, InheritGlobal: true}, "disabled-effects-inherit-layout"); err != nil {
		t.Fatal("disabled historical effects blocked inherited output layout", err)
	}
	if _, err := service.Validate(t.Context(), options); err == nil {
		t.Fatal("disabled server accepted an effect chain")
	}
	options.Chain = []audio.NodeSpec{}
	if _, err := service.Validate(t.Context(), options); err != nil {
		t.Fatal("disabled server rejected channel conversion", err)
	}
	f.request.Capabilities.Formats = append(f.request.Capabilities.Formats, playback.Format{MIME: "audio/webm; codecs=opus", CanPlay: "probably"})
	f.request.Preferences.DSPExecution = "client"
	f.request.Preferences.GainDB = -30
	description := f.create(t, "disabled-effects-surround")
	assertClientProcessing(t, description)
	if description.OutputProfile.Layout != "stereo" || !p.RequiresChannelConversion() || p.HasDSP() || len(p.DSPPlugins()) != 0 {
		t.Fatalf("disabled effects removed format conversion or retained plugins: %+v", description)
	}
	if _, err := processedBytes(t, f, description); err != nil {
		t.Fatal(err)
	}
	f.request.Preferences.DSPExecution = "server"
	server := f.create(t, "disabled-effects-server-request")
	if server.DSPExecution != "server" || len(server.EffectiveProcessing) != 1 || server.EffectiveProcessing[0].Type != "channelMix" {
		t.Fatalf("server request bypassed deployment effect disablement: %+v", server)
	}
	profile, err := f.player.RecoveryProfileForUser(t.Context(), f.who.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := timeline.Request{RecordingID: f.recording, From: timelineTarget(f.tracks[0]), To: timelineTarget(f.tracks[1]),
		Range: timeline.Range{EndMs: 6000}, OffsetMs: 1000, Profile: profile}
	request.Profile.GainDB = -6
	_, err = f.player.AddTimeline(t.Context(), f.who.User.ID, request, "disabled-effects-nonzero-evidence")
	recoveryError(t, err, "INVALID_ARGUMENT")
	request.Profile.GainDB = 0
	if _, err := f.player.AddTimeline(t.Context(), f.who.User.ID, request, "disabled-effects-zero-evidence"); err != nil {
		t.Fatal("disabled effects rejected matching zero-gain evidence", err)
	}
}

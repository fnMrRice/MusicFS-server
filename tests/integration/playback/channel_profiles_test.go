package playback_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	streamhttp "github.com/fnMrRice/MusicFS-server/src/playback/stream"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestSurroundPlaybackNegotiationCacheSeekAndRecovery(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual surround playback validation")
	}
	for _, layout := range []string{"5.1", "7.1"} {
		t.Run(layout, func(t *testing.T) {
			cfg := audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, OutputLayout: layout, MaxConcurrent: 1}
			processor, err := audioffmpeg.NewProcessor(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := processor.OutputProfile()
			if err != nil {
				t.Fatal(err)
			}
			f := recoverySetup(t, processor, 0)
			f.request.Preferences.ForceProcessing = false
			for index, format := range []string{"audio/mpeg", `audio/webm; codecs="vorbis"`} {
				request := f.request
				request.Capabilities.Formats = []playback.Format{{MIME: format, CanPlay: "probably"}}
				_, err = f.player.Create(t.Context(), f.who, request, []string{"surround-no-webm", "surround-wrong-codec"}[index])
				recoveryError(t, err, "UNSUPPORTED")
			}
			f.request.Capabilities.Formats = append(f.request.Capabilities.Formats, playback.Format{MIME: `audio/webm; codecs="opus"`, CanPlay: "probably"})
			description := f.create(t, "surround-output-create")
			if description.DeliveryMode != "processed" || description.OutputProfile.ID != profile.ID || description.OutputProfile.Channels != profile.Channels || description.OutputProfile.Layout != layout || description.OutputProfile.BitrateBps != profile.BitrateBps || description.OutputProfile.SampleRate != 48000 || description.OutputProfile.Codec == nil || *description.OutputProfile.Codec != "opus" {
				t.Fatal("surround deployment did not produce an explicit multichannel profile", description)
			}
			if len(description.EffectiveProcessing) != 2 || description.EffectiveProcessing[0].Type != "channelMix" || description.EffectiveProcessing[0].Parameters["outputChannels"] != float64(profile.Channels) || description.EffectiveProcessing[1].Type != "gain" {
				t.Fatal("channel conversion was not described before gain", description.EffectiveProcessing)
			}
			rawDescription, _ := json.Marshal(description)
			testkit.ValidateHTTP(t, "Playback", rawDescription)
			full, err := processedBytes(t, f, description)
			if err != nil {
				t.Fatal(err)
			}
			assertSurroundEncoded(t, ffprobe, full, profile.Channels)
			cached, err := processedBytes(t, f, description)
			if err != nil || !bytes.Equal(full, cached) {
				t.Fatal("multichannel cache did not reproduce the completed representation", err)
			}
			records, err := f.repo.ListProcessedCache(t.Context())
			if err != nil || len(records) != 1 || records[0].Identity.Profile != profile.ID {
				t.Fatal("cache did not bind the surround output profile", records, err)
			}
			stream, err := f.player.OpenStream(t.Context(), f.who, description.SessionID, description.AttemptID, false)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			if err = streamhttp.Serve(response, httptest.NewRequest("GET", description.StreamURL, nil), stream); err != nil || response.Header().Get("Content-Type") != "audio/webm" {
				t.Fatal("multichannel stream used the wrong HTTP content type", err, response.Header())
			}
			seek, err := f.player.Control(t.Context(), f.who, description.SessionID, "seek", playback.ControlRequest{AttemptID: description.AttemptID, PositionMs: 3000}, "surround-seek")
			if err != nil || seek.OutputProfile.ID != profile.ID {
				t.Fatal("seek lost the surround profile", seek, err)
			}
			tail, err := processedBytes(t, f, seek)
			if err != nil {
				t.Fatal(err)
			}
			assertSurroundEncoded(t, ffprobe, tail, profile.Channels)
			evidence := f.evidence(t, 1)
			result, err := f.player.RecoverSource(t.Context(), f.who, seek.SessionID, recoveryRequest(seek, f.tracks[1].ID, &evidence), "surround-recovery")
			if err != nil || result.Decision != "continue" || result.Playback == nil || result.Playback.OutputProfile.ID != profile.ID {
				t.Fatal("matching surround timeline could not recover", result, err)
			}
			recovered, err := processedBytes(t, f, *result.Playback)
			if err != nil {
				t.Fatal(err)
			}
			assertSurroundEncoded(t, ffprobe, recovered, profile.Channels)
			old := f.create(t, "surround-before-layout-change")
			cfg.OutputLayout = "stereo"
			stereo, err := audioffmpeg.NewProcessor(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			f.player.UpdateAudio(stereo, nil)
			result, err = f.player.RecoverSource(t.Context(), f.who, old.SessionID, recoveryRequest(old, f.tracks[1].ID, &evidence), "surround-changed-layout")
			if err != nil || result.Decision != "promptRestart" || result.Reason != "OUTPUT_PROFILE_UNAVAILABLE" {
				t.Fatal("layout change reused surround timeline evidence", result, err)
			}
			proxy := f.create(t, "stereo-original-create")
			if proxy.DeliveryMode != "proxy" || len(proxy.EffectiveProcessing) != 0 {
				t.Fatal("original audio incorrectly claimed channel conversion", proxy)
			}
		})
	}
}

func assertSurroundEncoded(t *testing.T, ffprobe string, encoded []byte, channels int) {
	t.Helper()
	command := exec.Command(ffprobe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,channels,sample_rate", "-of", "json", "pipe:0")
	command.Stdin = bytes.NewReader(encoded)
	raw, err := command.Output()
	var metadata struct {
		Streams []struct {
			Codec      string `json:"codec_name"`
			Channels   int    `json:"channels"`
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
	}
	if err != nil || json.Unmarshal(raw, &metadata) != nil || len(metadata.Streams) != 1 || metadata.Streams[0].Codec != "opus" || metadata.Streams[0].Channels != channels || metadata.Streams[0].SampleRate != "48000" {
		t.Fatalf("encoded output does not match the advertised surround profile: %s %v", raw, err)
	}
}

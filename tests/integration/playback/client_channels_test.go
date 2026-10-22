package playback_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestClientChannelsIgnoreLegacyLayoutsAndPreserveSource(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for source channel validation")
	}
	for _, tc := range []struct {
		layout   string
		channels int
	}{{"mono", 1}, {"stereo", 2}, {"5.1", 6}, {"7.1", 8}} {
		t.Run(tc.layout, func(t *testing.T) {
			processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe,
				DisableEffects: true, DisableChannelMix: true, OutputLayout: "7.1"})
			if err != nil {
				t.Fatal(err)
			}
			payload, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "anoisesrc=d=2:a=.1", "-ac", fmt.Sprint(tc.channels), "-channel_layout", tc.layout, "-c:a", "flac", "-f", "flac", "pipe:1").Output()
			if err != nil {
				t.Fatal(err)
			}
			f := metadataPlaybackFixture(t, "channels.flac", payload, processor, nil)
			legacy := dspsettings.Options{Chain: []audio.NodeSpec{{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}}}, OutputLayout: "7.1", BufferFrames: 4096}
			if _, err = f.repo.PutPluginSettings(t.Context(), f.who.User.ID, dspsettings.StorageID, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": legacy}); err != nil {
				t.Fatal(err)
			}
			original := f.create(t, "channels-original")
			if original.DeliveryMode != "proxy" || original.OutputProfile.Channels != tc.channels || len(original.EffectiveProcessing) != 0 {
				t.Fatal("historical layout changed compatible source", original)
			}
			f.request.Preferences.ForceProcessing = true
			f.request.Capabilities.Formats = append(f.request.Capabilities.Formats, playback.Format{MIME: "audio/webm; codecs=opus", CanPlay: "probably"})
			description := f.create(t, "channels-transcode")
			if description.OutputProfile.Channels != tc.channels || description.OutputProfile.Layout != tc.layout || len(description.EffectiveProcessing) != 0 {
				t.Fatal("transcode did not retain source channels", description)
			}
			encoded, err := processedBytes(t, f, description)
			if err != nil {
				t.Fatal(err)
			}
			if tc.channels > 2 {
				assertSurroundEncoded(t, ffprobe, encoded, tc.channels)
			}
			cached, err := processedBytes(t, f, description)
			if err != nil || !bytes.Equal(encoded, cached) {
				t.Fatal("source channel cache changed output", err)
			}
			// 禁用标记同时约束服务端执行归属，不只是 Web 请求的临时旁路。
			f.request.Preferences.DSPExecution = "server"
			server := f.create(t, "channels-disabled-server")
			if server.OutputProfile.Channels != tc.channels || len(server.EffectiveProcessing) != 0 {
				t.Fatal("server request reenabled channel plugin", server)
			}
			if tc.channels > 2 {
				f.request.Capabilities.Formats = []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}
				fallback := f.create(t, "channels-mp3-only")
				if fallback.OutputProfile.Channels != 2 || fallback.OutputProfile.MIME != "audio/mpeg" {
					t.Fatal("missing compatible downmix", fallback)
				}
				if _, err := processedBytes(t, f, fallback); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

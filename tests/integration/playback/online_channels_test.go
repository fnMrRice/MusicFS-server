package playback_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	streamhttp "github.com/fnMrRice/MusicFS-server/src/playback/stream"
)

func TestOnlinePlaybackMeasuredChannelCounts(t *testing.T) {
	processor := flacTestProcessor(t, "stereo")
	for _, tc := range []struct {
		codec, container, mime string
		channels               int
	}{
		{"mp3", "mp3", "audio/mpeg", 1}, {"flac", "flac", "audio/flac", 6}, {"aac", "m4a", "audio/mp4", 1},
	} {
		t.Run(tc.codec, func(t *testing.T) {
			f := newExplicitDownload(t)
			f.player.UpdateAudio(processor, nil)
			ctx := t.Context()
			accounts := auth.New(f.store, "fixture-token", []string{"http://client.test"}, time.Hour)
			who, _, err := accounts.Login(ctx, "fixture-token", "http://client.test", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "audio."+tc.container)
			command := exec.CommandContext(ctx, os.Getenv("MUSICFS_FFMPEG"), "-v", "error", "-f", "lavfi", "-i", "anullsrc=r=48000", "-t", "2", "-ac", fmt.Sprint(tc.channels), path)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("encode audio: %s %v", output, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			duration := int64(2000)
			f.describe = func(context.Context, string) (pluginproto.OnlineAudio, error) {
				return pluginproto.OnlineAudio{ExternalID: "42", MIME: tc.mime, Codec: tc.codec, Container: tc.container, SizeBytes: int64(len(data)), DurationMs: &duration, Version: "v1"}, nil
			}
			f.body = func() io.ReadCloser { return io.NopCloser(bytes.NewReader(data)) }
			d, err := f.player.Create(ctx, who, playback.CreateRequest{SourceTrackID: f.track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: tc.mime, CanPlay: "probably"}}}, Preferences: playback.Preferences{DSPExecution: "client"}}, "channels-"+tc.codec)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := f.player.AttemptOutputProfile(ctx, who, d.SessionID, d.AttemptID)
			if err != nil || profile.Channels != 0 || !d.OutputProfilePending || f.opened.Load() != 0 {
				t.Fatalf("unexpected unopened profile: %+v, %v", profile, err)
			}
			s, err := f.player.OpenStream(ctx, who, d.SessionID, d.AttemptID, false)
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			if err = streamhttp.Serve(w, httptest.NewRequest("GET", d.StreamURL, nil), s); err != nil {
				t.Fatal(err)
			}
			profile, err = f.player.AttemptOutputProfile(ctx, who, d.SessionID, d.AttemptID)
			if err != nil || profile.Channels != tc.channels || f.opened.Load() != 1 || !bytes.Equal(data, w.Body.Bytes()) {
				t.Fatalf("expected %d channels, got %+v, %v; opens=%d", tc.channels, profile, err, f.opened.Load())
			}
		})
	}
}

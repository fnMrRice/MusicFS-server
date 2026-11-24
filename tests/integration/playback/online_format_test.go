package playback_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	streamhttp "github.com/fnMrRice/MusicFS-server/src/playback/stream"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlinePlaybackFormatFollowsCurrentRendition(t *testing.T) {
	f := newExplicitDownload(t)
	ctx := t.Context()
	accounts := auth.New(f.store, "fixture-token", []string{"http://client.test"}, time.Hour)
	who, _, err := accounts.Login(ctx, "fixture-token", "http://client.test", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	type rendition struct {
		mime, codec, file string
		bitrate           int64
		measuredBitrate   int
		bitDepth          int
		sampleRate        int
		content           []byte
	}
	mp3 := &rendition{mime: "audio/mpeg", codec: "mp3", file: "vbr-untagged.mp3", bitrate: 128000, measuredBitrate: 33266}
	flac := &rendition{mime: "audio/flac", codec: "flac", file: "lossless.flac", bitrate: 717577, sampleRate: 48000, measuredBitrate: 116163, bitDepth: 16}
	// VBR 平均码率可因探测器是否计入 Xing 头帧而有轻微差异。
	closeBitrate := func(got, want int) bool { return got >= want-128 && got <= want+128 }
	for _, r := range []*rendition{mp3, flac} {
		r.content, err = os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio", r.file))
		if err != nil {
			t.Fatal(err)
		}
	}
	f.describe = func(ctx context.Context, id string) (pluginproto.OnlineAudio, error) {
		r, quality := mp3, "standard"
		if provider.PlaybackQuality(ctx) == "hires" {
			r, quality = flac, "lossless"
		}
		// 该平台把无损文件也声明为 MP3，完整文件的实际编码应覆盖播放与响应头的提示。
		return pluginproto.OnlineAudio{ExternalID: id, MIME: mp3.mime, Codec: mp3.codec, Container: mp3.codec,
			Quality: quality, Version: r.file, SizeBytes: int64(len(r.content)), BitrateBps: &r.bitrate, SampleRateHz: r.sampleRate}, nil
	}
	// 两份真实编码样本依次由上游提供；切回首个版本时必须命中对应缓存。
	for i, step := range []struct {
		quality string
		want    *rendition
		opens   int32
	}{
		{"standard", mp3, 1}, {"hires", flac, 2}, {"standard", mp3, 2}, {"hires", flac, 2},
	} {
		want := step.want
		f.body = func() io.ReadCloser { return io.NopCloser(bytes.NewReader(want.content)) }
		if _, err := f.store.PutPluginSettings(ctx, "admin", "test-online", int64(i), settings.Values{
			"playbackQuality": step.quality, "streamWhileDownloading": i == 0,
		}); err != nil {
			t.Fatal(err)
		}
		caps := playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: mp3.mime, CanPlay: "probably"}, {MIME: flac.mime, CanPlay: "probably"}}}
		d, err := f.player.Create(ctx, who, playback.CreateRequest{SourceTrackID: f.track.ID, Capabilities: caps}, fmt.Sprintf("play-format-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		out := d.OutputProfile
		expectFormat := want
		if i < 2 {
			expectFormat = mp3
		}
		if d.SourceTrackID != f.track.ID || d.DeliveryMode != "proxy" || out.MIME != expectFormat.mime ||
			out.Codec == nil || *out.Codec != expectFormat.codec || out.Container == nil || *out.Container != expectFormat.codec {
			t.Fatalf("playback of %s reported %+v", step.quality, d)
		}
		if i < 2 {
			if !d.OutputProfilePending || out.SampleRate != want.sampleRate || out.Channels != 0 || out.BitrateBps != int(want.bitrate) {
				t.Fatalf("uncached rendition reported %+v", d)
			}
		} else if d.OutputProfilePending || out.SampleRate != 48000 || out.Channels != 2 || !closeBitrate(out.BitrateBps, want.measuredBitrate) || out.BitDepth != want.bitDepth {
			t.Fatalf("cached rendition reported %+v", d)
		}
		s, err := f.player.OpenStream(ctx, who, d.SessionID, d.AttemptID, false)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		if err = streamhttp.Serve(w, httptest.NewRequest(http.MethodGet, d.StreamURL, nil), s); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != want.mime || !bytes.Equal(w.Body.Bytes(), want.content) || f.opened.Load() != step.opens {
			t.Fatalf("wrong %s response or cache selection: status=%d type=%s opens=%d", step.quality, w.Code, w.Header().Get("Content-Type"), f.opened.Load())
		}
		got, err := f.player.AttemptOutputProfile(ctx, who, d.SessionID, d.AttemptID)
		if err != nil || got.MIME != want.mime || got.Codec == nil || *got.Codec != want.codec || got.Channels != 2 || f.opened.Load() != step.opens {
			t.Fatalf("online output probe returned %+v, %v; upstream requests: %d", got, err, f.opened.Load())
		}
		_, err = f.player.AttemptOutputProfile(ctx, who, d.SessionID, "stale-attempt")
		recoveryError(t, err, "CONFLICT")
		_, err = f.player.AttemptOutputProfile(ctx, auth.Session{}, d.SessionID, d.AttemptID)
		recoveryError(t, err, "NOT_FOUND")
		if err = f.player.Stop(ctx, who, d.SessionID); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.player.Create(ctx, who, playback.CreateRequest{SourceTrackID: f.track.ID,
		Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: mp3.mime, CanPlay: "probably"}}}}, "unsupported-cached-flac")
	recoveryError(t, err, "UNSUPPORTED")
	if f.opened.Load() != 2 {
		t.Fatal("checking the cached format reopened the upstream media")
	}
	track, err := f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || !track.Downloaded || len(track.DownloadVersions) != 2 || track.Codec != nil || track.Container != nil || track.BitrateBps != nil {
		t.Fatalf("unexpected catalog source after playback: %+v, %v", track, err)
	}
	wantVersions := map[string]*rendition{"standard": mp3, "lossless": flac}
	for _, v := range track.DownloadVersions {
		r := wantVersions[v.Quality]
		if r == nil || !v.FormatMeasured || v.Codec != r.codec || v.Container != r.codec || v.MIME != r.mime || v.BitrateBps == nil ||
			!closeBitrate(int(*v.BitrateBps), r.measuredBitrate) || v.Channels != 2 || v.SampleRateHz != 48000 || v.BitDepth != r.bitDepth {
			t.Fatalf("incorrect saved rendition: %+v", v)
		}
		delete(wantVersions, v.Quality)
	}
}

func TestOnlinePlaybackFormatRejectsChangedSampleRate(t *testing.T) {
	for _, changeAt := range []int{2, 3} {
		t.Run(fmt.Sprintf("describe-call-%d", changeAt), func(t *testing.T) {
			f := newExplicitDownload(t)
			ctx := t.Context()
			accounts := auth.New(f.store, "fixture-token", []string{"http://client.test"}, time.Hour)
			who, _, err := accounts.Login(ctx, "fixture-token", "http://client.test", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			describe, calls := f.describe, 0
			f.describe = func(ctx context.Context, id string) (pluginproto.OnlineAudio, error) {
				audio, err := describe(ctx, id)
				calls++
				audio.SampleRateHz = 44100
				if calls >= changeAt {
					audio.SampleRateHz = 48000
				}
				return audio, err
			}
			d, err := f.player.Create(ctx, who, playback.CreateRequest{SourceTrackID: f.track.ID,
				Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/flac", CanPlay: "probably"}}}}, "changing-sample-rate")
			if err != nil {
				t.Fatal(err)
			}
			stream, err := f.player.OpenStream(ctx, who, d.SessionID, d.AttemptID, false)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			if body, err := stream.OpenOnline(); body != nil || pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
				t.Fatalf("sample rate changed on call %d: %v", changeAt, err)
			}
		})
	}
}

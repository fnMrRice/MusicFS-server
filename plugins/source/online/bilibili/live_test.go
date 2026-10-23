package bilibili

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// 只有显式开启时访问公开端点，不读取浏览器账户、Cookie 或用户配置。
func TestLiveAnonymousMetadata(t *testing.T) {
	if os.Getenv("MUSICFS_BILIBILI_LIVE") != "1" {
		t.Skip("Set MUSICFS_BILIBILI_LIVE=1 to verify public upstream metadata")
	}
	c, err := New(online.Scope{UserID: "live-test", InstanceID: "live-test"}, Options{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, id := range []string{"BV13x41117TL", "av8903802", "https://www.bilibili.com/video/BV1bK411W797?p=2", "au1003142"} {
		t.Run(id, func(t *testing.T) {
			track, err := c.Metadata(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := online.ValidateTrack(Plugin(Options{}).Descriptor.Resources, track); err != nil {
				t.Fatal(err)
			}
			t.Logf("metadata verified: %s", track.ExternalID)
		})
	}
	for _, id := range []string{"favorite:1103407912", "audio-menu:10624"} {
		t.Run(id, func(t *testing.T) {
			observation, err := c.ObservePlaylist(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if err := online.ValidateObservation(Plugin(Options{}).Descriptor.Resources, observation, id); err != nil {
				t.Fatal(err)
			}
			t.Logf("collection verified: %s entries=%d complete=%t", id, len(observation.Entries), observation.Complete)
		})
	}
}

// 播放描述验证只调用 JSON 接口，不请求 CDN，也不读取音频内容。
func TestLiveAnonymousAudioMetadata(t *testing.T) {
	if os.Getenv("MUSICFS_BILIBILI_LIVE") != "1" {
		t.Skip("Set MUSICFS_BILIBILI_LIVE=1 to verify public upstream metadata")
	}
	c, err := New(online.Scope{UserID: "live-test", InstanceID: "live-test"}, Options{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, id := range []string{"video:BV13x41117TL:14694589", "audio:1003142"} {
		t.Run(id, func(t *testing.T) {
			r, err := parseReference(id)
			if err != nil {
				t.Fatal(err)
			}
			choice, err := c.chooseAudio(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if !safeCDN(choice.url) {
				t.Fatal("unsafe audio destination")
			}
			t.Logf("audio metadata verified: %s size=%d bitrate=%d", id, choice.size, choice.bitrate)
		})
	}
}

// 首块检查需要另外显式开启，最多读取 16 KiB 后立即关闭，不下载完整音轨。
func TestLiveAnonymousAudioFirstChunk(t *testing.T) {
	if os.Getenv("MUSICFS_BILIBILI_STREAM_LIVE") != "1" {
		t.Skip("Set MUSICFS_BILIBILI_STREAM_LIVE=1 to verify one bounded public audio chunk")
	}
	c, err := New(online.Scope{UserID: "live-test", InstanceID: "live-test"}, Options{Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const id = "video:BV13x41117TL:14694589"
	audio, body, err := c.OpenAudio(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 16<<10)
	n, readErr := io.ReadFull(body, buffer)
	closeErr := body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if n != len(buffer) || string(buffer[4:8]) != "ftyp" {
		t.Fatal("CDN did not return the expected MP4 initialization bytes")
	}
	t.Logf("bounded audio chunk verified: %s bytes=%d mime=%s codec=%s; body closed", id, n, audio.MIME, audio.Codec)
}

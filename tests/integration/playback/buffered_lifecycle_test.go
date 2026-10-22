package playback_test

import (
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestBufferedPlaybackRetainsSessionAfterStreamCloses(t *testing.T) {
	for _, processed := range []bool{false, true} {
		name := "original"
		if processed {
			name = "processed"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				service, _, who, request, _ := playbackService(t)
				request.Preferences.ForceProcessing = processed
				d, err := service.Create(context.Background(), who, request, "buffered-playback")
				if err != nil {
					t.Fatal(err)
				}
				stream, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false)
				if err != nil {
					t.Fatal(err)
				}
				if !processed {
					if _, err := io.Copy(io.Discard, stream.File); err != nil {
						stream.Close()
						t.Fatal(err)
					}
				}
				// 原流已全部读取，或处理流提前断开；浏览器仍可能保有音频缓冲。
				stream.Close()
				time.Sleep(2 * time.Minute)
				synctest.Wait()
				if processed {
					recovered, err := service.Control(context.Background(), who, d.SessionID, "recover", playback.ControlRequest{
						AttemptID: d.AttemptID, PositionMs: 2500,
						ObservedError: &playback.ObservedError{Code: "MEDIA_ERROR_2", Message: "Buffered stream interrupted"},
					}, "buffered-recover")
					if err != nil {
						t.Fatal("buffered playback could not recover after two minutes without a stream", err)
					}
					if recovered.SessionID != d.SessionID || recovered.AttemptID == d.AttemptID || recovered.PositionOriginMs != 2500 || !recovered.ExpiresAt.Equal(d.ExpiresAt) {
						t.Fatalf("recovery changed the session lifetime or lost the playback position: %+v", recovered)
					}
					d = recovered
				}
				next, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false)
				if err != nil {
					t.Fatal("buffered playback session was prematurely removed", err)
				}
				next.Close()
			})
		})
	}
}

func TestBufferedPlaybackStillExpiresAndReleasesSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		service, repo, who, request, _ := playbackService(t)
		d, err := service.Create(context.Background(), who, request, "buffered-expiry")
		if err != nil {
			t.Fatal(err)
		}
		if lifetime := time.Until(d.ExpiresAt); lifetime > 30*time.Minute || lifetime <= 29*time.Minute {
			t.Fatalf("unexpected playback lifetime: %s", lifetime)
		}
		stream, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false)
		if err != nil {
			t.Fatal(err)
		}
		stream.Close()
		time.Sleep(time.Until(d.ExpiresAt) + time.Second)
		synctest.Wait()
		if stale, err := service.OpenStream(context.Background(), who, d.SessionID, d.AttemptID, false); err == nil {
			stale.Close()
			t.Fatal("expired buffered session remained accessible")
		}
		lease, err := repo.library.TryExclusive(context.Background(), "admin", "root", domain.NewID())
		if err != nil {
			t.Fatal("expired buffered session retained its source lease", err)
		}
		lease.Close()
	})
}

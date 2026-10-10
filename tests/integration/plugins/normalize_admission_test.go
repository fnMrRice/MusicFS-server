package plugins_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
)

type waitingAudioInput struct {
	ctx     context.Context
	entered chan struct{}
	reads   atomic.Int64
}

func (r *waitingAudioInput) ReadAt([]byte, int64) (int, error) {
	if r.reads.Add(1) == 1 {
		close(r.entered)
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestFingerprintNormalizationReservesPlaybackCapacity(t *testing.T) {
	p := newProcessor(t) // One foreground slot is enough while analysis is active.
	if !p.Capabilities.Normalization {
		t.Skip("FFmpeg lacks PCM normalization components")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	blocked := &waitingAudioInput{ctx: ctx, entered: make(chan struct{})}
	request := audio.NormalizeRequest{TaskID: "background", AssetID: "asset", DurationMs: 10000}
	done := make(chan error, 1)
	go func() {
		_, err := p.Normalize(ctx, request, blocked, 1<<20, io.Discard)
		done <- err
	}()
	select {
	case <-blocked.entered:
	case err := <-done:
		t.Fatalf("normalization ended before reading input: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("normalization worker did not start")
	}

	// A second background task waits without launching another resource reader.
	waitCtx, stopWaiting := context.WithTimeout(context.Background(), 150*time.Millisecond)
	waiter := &waitingAudioInput{ctx: waitCtx, entered: make(chan struct{})}
	_, err := p.Normalize(waitCtx, request, waiter, 1<<20, io.Discard)
	stopWaiting()
	if !errors.Is(err, context.DeadlineExceeded) || waiter.reads.Load() != 0 {
		t.Fatalf("background admission did not wait within its deadline: %v", err)
	}

	wave := toneWave(4)
	var output bytes.Buffer
	playCtx, stopPlayback := context.WithTimeout(context.Background(), 15*time.Second)
	err = p.Process(playCtx, audioffmpeg.Plan{TaskID: "foreground", AssetID: "playback", Profile: "mp3-128"}, bytes.NewReader(wave), int64(len(wave)), &output)
	stopPlayback()
	if err != nil || output.Len() == 0 {
		t.Fatalf("background normalization consumed playback capacity: %v", err)
	}

	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled normalization: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("normalization cancellation did not reclaim worker")
	}
	// Cancellation also returns the dedicated background admission slot.
	if _, err = p.Normalize(context.Background(), request, bytes.NewReader(wave), int64(len(wave)), io.Discard); err != nil {
		t.Fatalf("normalization slot was not reusable: %v", err)
	}
}

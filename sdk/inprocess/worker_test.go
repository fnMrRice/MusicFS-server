package inprocess_test

import (
	"context"
	"io"
	"testing"
	"time"

	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestWorkersKeepIndependentChannelsAndCancelBlockedIO(t *testing.T) {
	first := inprocess.Start(context.Background(), func(ctx context.Context, r io.ReadCloser, w io.WriteCloser) error {
		_, err := io.Copy(w, r)
		return err
	})
	second := inprocess.Start(context.Background(), func(ctx context.Context, r io.ReadCloser, w io.WriteCloser) error {
		_, err := w.Write([]byte("blocked"))
		return err
	})
	go func() { _, _ = first.Input.Write([]byte("first")) }()
	data := make([]byte, 5)
	if _, err := io.ReadFull(first.Output, data); err != nil || string(data) != "first" {
		t.Fatalf("unexpected independent worker output: %q, %v", data, err)
	}
	for _, worker := range []*inprocess.Worker{first, second} {
		_ = worker.Close()
		_ = worker.Close()
		select {
		case <-worker.Done:
		case <-time.After(time.Second):
			t.Fatal("worker did not stop after channel cancellation")
		}
	}
}

func TestPeerWaitDrainsCanceledHandlerBeforeWorkerStops(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	worker := inprocess.Start(t.Context(), func(ctx context.Context, r io.ReadCloser, w io.WriteCloser) error {
		peer := pluginproto.NewPeer(ctx, r, w, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			defer close(finished)
			return nil, ctx.Err()
		})
		peer.Wait()
		return peer.Err()
	})
	peer := pluginproto.NewPeer(t.Context(), worker.Output, worker.Input, "h-", nil)
	go func() { _ = peer.Call(t.Context(), "wait", nil, nil) }()
	<-started
	_ = worker.Close()
	select {
	case <-worker.Done:
		select {
		case <-finished:
		default:
			t.Fatal("worker stopped before its request handler exited")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request handler prevented worker shutdown")
	}
	_ = peer.Close()
}

func TestParentCancellationClosesWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	worker := inprocess.Start(ctx, func(ctx context.Context, r io.ReadCloser, w io.WriteCloser) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	cancel()
	select {
	case <-worker.Done:
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not release blocked read")
	}
}

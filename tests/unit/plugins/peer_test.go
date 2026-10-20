package plugins_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestPeerCancellationLateResultAndSubsequentCall(t *testing.T) {
	left, right := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	server := pluginproto.NewPeer(ctx, right, right, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "slow" {
			<-ctx.Done()
			return "late", nil
		}
		return "ok", nil
	})
	defer server.Close()
	client := pluginproto.NewPeer(ctx, left, left, "h-", nil)
	defer client.Close()
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if err := client.Call(short, "slow", nil, nil); err == nil {
		t.Fatal("canceled request succeeded")
	}
	var value string
	if err := client.Call(ctx, "fast", nil, &value); err != nil || value != "ok" {
		t.Fatalf("subsequent request failed: %q %v", value, err)
	}
}

func TestPeerRejectsDuplicateTerminalResponse(t *testing.T) {
	left, right := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := pluginproto.NewPeer(ctx, left, left, "h-", nil)
	defer client.Close()
	defer right.Close()
	accepted := make(chan struct{})
	go func() {
		framer := pluginproto.NewFramer(right, right)
		request, err := framer.Read()
		if err != nil {
			return
		}
		response := pluginproto.Envelope{ID: request.ID, Result: json.RawMessage(`"ok"`)}
		framer.Write(response)
		// Only send the duplicate after Call has observed the valid result. If
		// both arrive together, rejecting the whole peer may correctly win first.
		select {
		case <-accepted:
		case <-ctx.Done():
			return
		}
		framer.Write(response)
	}()
	if err := client.Call(ctx, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	close(accepted)
	select {
	case <-client.Done():
		if client.Err() == nil || client.Err() == io.EOF {
			t.Fatal("duplicate result did not produce protocol error")
		}
	case <-ctx.Done():
		t.Fatal("duplicate result not rejected")
	}
}

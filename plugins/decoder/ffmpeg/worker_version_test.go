package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
)

func TestOldAudioWorkerHelper(t *testing.T) {
	if !slices.Contains(os.Args, "--old-audio-worker") {
		return
	}
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		if method != "host.hello" {
			return nil, fmt.Errorf("old worker received unsupported operation")
		}
		var hello bootstrap
		if err := json.Unmarshal(raw, &hello); err != nil {
			return nil, err
		}
		return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{
			PluginID: "musicfs.plugin.decoder.ffmpeg", Plugin: pluginconfig.Identity("musicfs.plugin.decoder.ffmpeg"),
			PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion,
			Roles: []string{"audio"}, Capabilities: []string{"audio.decode", "audio.dsp", "audio.encode", "audio.mux"},
			Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH},
			MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems,
		}}, nil
	})
	<-peer.Done()
	os.Exit(0)
}

func TestOldAudioWorkerRejectedBeforeProcessing(t *testing.T) {
	if audioPluginVersion != "0.2.0" {
		t.Fatal("DSP-aware worker requires implementation version 0.2.0")
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := executableHash(path)
	if err != nil {
		t.Fatal(err)
	}
	p := &Processor{config: Config{
		InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea",
		WorkerPath: path, WorkerSHA256: hash, WorkDir: t.TempDir(),
		WorkerArgs: []string{"-test.run=^TestOldAudioWorkerHelper$", "--", "--old-audio-worker"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	called := false
	err = p.run(ctx, "reject-old-worker", bytes.NewReader([]byte{1}), 1, io.Discard, func(*pluginproto.Peer, string, string) error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "audio worker implementation differs") || called {
		t.Fatalf("legacy worker was not rejected before audio execution: called=%v error=%v", called, err)
	}
}

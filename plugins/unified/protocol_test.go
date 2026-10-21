package unified_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/unified"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func selectedPeer(t *testing.T, role string) (*pluginproto.Peer, context.Context) {
	t.Helper()
	worker, err := unified.Select([]string{"--plugin", role})
	if err != nil {
		t.Fatal(err)
	}
	host, child := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan struct{})
	go func() {
		_ = worker(ctx, child, child)
		close(done)
	}()
	peer := pluginproto.NewPeerWithLimit(ctx, host, host, "h-", nil, pluginrpc.MaxMessageBytes)
	t.Cleanup(func() {
		peer.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("selected worker did not stop")
		}
	})
	return peer, ctx
}

func TestSelectedOnlineRolesRetainTheirOwnIdentity(t *testing.T) {
	for _, platform := range []string{"netease", "qqmusic", "applemusic"} {
		t.Run(platform, func(t *testing.T) {
			peer, ctx := selectedPeer(t, platform)
			hello := pluginproto.OnlineHello{
				Hello: pluginproto.Hello{
					ProtocolVersion: pluginproto.ProtocolVersion, ConfigRevision: pluginproto.OnlineRevision,
					InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", Nonce: strings.Repeat("a", 48),
				},
				UserID: "owner", TimeoutMs: 1000,
			}
			var ready pluginproto.Ready
			if err := peer.Call(ctx, "host.hello", hello, &ready); err != nil {
				t.Fatal(err)
			}
			if ready.InstanceID != hello.InstanceID || ready.Nonce != hello.Nonce {
				t.Fatal("selected platform lost the handshake binding")
			}
			if err := pluginproto.ValidateOnlineManifest(ready.Manifest, platform); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectedSQLiteInitializesPersistenceThroughItsProtocol(t *testing.T) {
	directory := t.TempDir()
	peer, ctx := selectedPeer(t, "storage-sqlite")
	fingerprint := pluginrpc.Fingerprint(reflect.TypeFor[storage.Backend]())
	nonce := strings.Repeat("b", 48)
	request := map[string]any{
		"Protocol": pluginrpc.Protocol, "Contract": fingerprint, "Nonce": nonce,
		"Mode": "backend", "Options": storage.Options{DataRoot: directory},
	}
	var ready map[string]string
	if err := peer.Call(ctx, "persistence.hello", request, &ready); err != nil {
		t.Fatal(err)
	}
	if ready["Protocol"] != pluginrpc.Protocol || ready["Contract"] != fingerprint || ready["Nonce"] != nonce {
		t.Fatal("selected persistence role returned the wrong contract")
	}
	if _, err := os.Stat(filepath.Join(directory, "musicfs.sqlite")); err != nil {
		t.Fatal("selected persistence role did not initialize SQLite", err)
	}
}

type protocolBuffer struct{ bytes.Buffer }

func (*protocolBuffer) Close() error { return nil }

func TestSelectedGainServesTheBinaryDSPProtocol(t *testing.T) {
	worker, err := unified.Select([]string{"--plugin", "dsp-gain"})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"protocolVersion": dspworker.ProtocolVersion, "nodeVersion": audio.NodeVersion,
		"name": "musicfs.dsp.gain", "format": audio.Canonical,
		"params": map[string]float64{"gainDB": -6},
	})
	if err != nil {
		t.Fatal(err)
	}
	header := [12]byte{'M', 'F', 'D', '1'}
	binary.LittleEndian.PutUint32(header[4:], 1)
	binary.LittleEndian.PutUint32(header[8:], uint32(len(body)))
	input := bytes.NewBuffer(header[:])
	input.Write(body)
	var output protocolBuffer
	if err = worker(context.Background(), io.NopCloser(input), &output); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(&output, header[:]); err != nil {
		t.Fatal(err)
	}
	if string(header[:4]) != "MFD1" || binary.LittleEndian.Uint32(header[4:]) != 1 {
		t.Fatal("selected gain role did not use the DSP protocol")
	}
	var ready struct {
		Name      string `json:"name"`
		Stateless bool   `json:"stateless"`
	}
	if err = json.Unmarshal(output.Bytes(), &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Name != "musicfs.dsp.gain" || !ready.Stateless {
		t.Fatal("selected gain role advertised the wrong node")
	}
}

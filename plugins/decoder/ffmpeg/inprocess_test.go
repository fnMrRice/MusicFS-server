package audioffmpeg

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestBuiltinAudioHandshakeNeedsNoWorkerExecutable(t *testing.T) {
	p := &Processor{config: Config{Builtin: true, InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea"}, Capabilities: Capabilities{Version: "test-tool-version"}}
	completed := fmt.Errorf("fixture operation completed")
	err := p.run(t.Context(), "builtin-audio", bytes.NewReader([]byte{1}), 1, io.Discard, func(peer *pluginproto.Peer, _, _ string) error {
		var capabilities Capabilities
		if err := peer.Call(t.Context(), "audio.describe", nil, &capabilities); err != nil {
			return err
		}
		if capabilities.Version != "test-tool-version" {
			t.Fatal("builtin audio worker lost task configuration")
		}
		return completed
	})
	if err != completed || p.PluginVersion() != audioPluginVersion {
		t.Fatalf("builtin audio handshake failed: %v, %s", err, p.PluginVersion())
	}
}

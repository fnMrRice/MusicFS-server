package plugins_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestDirectoryProviderWithoutRebindCapabilityKeepsPrivateState(t *testing.T) {
	const id = "test.source.private-state"
	identity := &pluginproto.PluginIdentity{ID: id, Name: "Private state provider", LocalizedNames: map[string]string{}, Features: []string{"music-source"}}
	if err := pluginconfig.Register(pluginconfig.Declaration{ID: id, Version: "1.0.0", Role: "provider", SourceType: pluginproto.SourceLocal, Directories: true, Identity: identity, Configuration: pluginconfig.LocalFiles}); err != nil {
		t.Fatal(err)
	}
	if err := pluginhost.RegisterBuiltin(id, func(ctx context.Context, in io.ReadCloser, out io.WriteCloser) error {
		var peer *pluginproto.Peer
		peer = pluginproto.NewPeer(ctx, in, out, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
			if method != "host.hello" {
				return registeredDirectoryCall(ctx, peer, method, raw)
			}
			var hello pluginproto.Hello
			if err := json.Unmarshal(raw, &hello); err != nil {
				return nil, err
			}
			return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: id, Plugin: identity, PluginVersion: "1.0.0", ProtocolVersion: pluginproto.ProtocolVersion, SourceType: pluginproto.SourceLocal, Roles: []string{"provider"}, Capabilities: []string{"library.packages"}, Configuration: pluginconfig.Configuration(id), MaxPageItems: pluginproto.MaxPageItems, MaxMessageBytes: pluginproto.MaxMessageBytes}}, nil
		})
		peer.Wait()
		return peer.Err()
	}); err != nil {
		t.Fatal(err)
	}
	root, state := t.TempDir(), t.TempDir()
	wave(t, filepath.Join(root, "song.wav"))
	private := filepath.Join(state, "plugin-state")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(private, "host-rebind-intent.json")
	value := []byte(`{"Revision":1,"Value":{"private":true}}`)
	if err := os.WriteFile(journal, value, 0600); err != nil {
		t.Fatal(err)
	}
	worker, err := pluginhost.Start(t.Context(), pluginhost.Config{PluginID: id, Builtin: true, InstanceID: "c847845e-d6fb-5368-b0fe-47783ad6a765", UserID: "owner", StateDir: state, Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	entry, err := worker.Access.Inspect("music", "song.wav")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.ObservePackage(t.Context(), []pluginproto.Entry{entry}); err != nil {
		t.Fatal("host interpreted private state without the identity capability", err)
	}
	if _, err := worker.RebindLocalTrack(t.Context(), pluginhost.LocalRebind{}); pluginproto.ErrorCode(err) != "UNSUPPORTED" {
		t.Fatal("provider without identity protocol accepted rebind", err)
	}
	actual, err := os.ReadFile(journal)
	if err != nil || string(actual) != string(value) {
		t.Fatal("host rewrote private provider state", err)
	}
}

package plugins_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestRegisteredDirectoryProviderUsesHostConfigurationAndPackageAccess(t *testing.T) {
	ctx := context.Background()
	const id = "test.source.directory"
	const instance = "b1548e96-3081-5c9c-8376-bf290e369d51"
	identity := &pluginproto.PluginIdentity{ID: id, Name: "Test directory", LocalizedNames: map[string]string{}, Features: []string{"music-source"}}
	declaration := pluginconfig.LocalFiles()
	declaration.SchemaRevision = "test-directory-v1"
	declaration.ImportDefaults.MaxDepth = 2
	err := pluginconfig.Register(pluginconfig.Declaration{ID: id, Version: "1.0.0", Role: "provider", SourceType: pluginproto.SourceLocal, Directories: true, Identity: identity, Configuration: func() *pluginproto.Configuration { return declaration }})
	if err != nil {
		t.Fatal(err)
	}
	declaration.ImportDefaults.MaxDepth = 9
	if pluginconfig.Configuration(id).ImportDefaults.MaxDepth != 2 {
		t.Fatal("registration retained caller-owned configuration")
	}
	err = pluginhost.RegisterBuiltin(id, func(ctx context.Context, in io.ReadCloser, out io.WriteCloser) error {
		var peer *pluginproto.Peer
		peer = pluginproto.NewPeer(ctx, in, out, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
			if method != "host.hello" {
				return registeredDirectoryCall(ctx, peer, method, raw)
			}
			var hello pluginproto.Hello
			if err := json.Unmarshal(raw, &hello); err != nil {
				return nil, err
			}
			return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: id, Plugin: identity, PluginVersion: "1.0.0", ProtocolVersion: pluginproto.ProtocolVersion, SourceType: pluginproto.SourceLocal, Roles: []string{"provider"}, Capabilities: []string{"library.packages", directoryidentity.Capability}, Configuration: pluginconfig.Configuration(id), MaxPageItems: pluginproto.MaxPageItems, MaxMessageBytes: pluginproto.MaxMessageBytes}}, nil
		})
		peer.Wait()
		return peer.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "album"), 0700); err != nil {
		t.Fatal(err)
	}
	worker, err := pluginhost.Start(ctx, pluginhost.Config{PluginID: id, Builtin: true, InstanceID: instance, UserID: "admin", StateDir: t.TempDir(), Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	lib := library.New(nil, jobs.New(nil), []library.Runtime{{ID: instance, Owner: "admin", Worker: worker, Configuration: pluginconfig.Configuration(id), ImportDefaults: pluginconfig.Configuration(id).ImportDefaults}}, nil)
	if _, err := lib.BrowsePackageDirectories(ctx, "admin", instance, "music", "."); err != nil {
		t.Fatal("package access still depends on the official plugin ID", err)
	}
	if _, err := lib.BrowsePackageDirectories(ctx, "other", instance, "music", "."); err == nil {
		t.Fatal("custom provider bypassed owner authorization")
	}
	file := filepath.Join(root, "album", "song.wav")
	wave(t, file)
	entry, err := worker.Access.Inspect("music", "album/song.wav")
	if err != nil {
		t.Fatal(err)
	}
	observations, err := worker.ObservePackage(ctx, []pluginproto.Entry{entry})
	if err != nil || len(observations) != 1 || observations[0].ExternalID != "independent-track" || observations[0].SourceRef != "opaque:asset/7" {
		t.Fatal("package observations require the official identity format", observations, err)
	}
	if err := os.Rename(file, file+".backup"); err != nil {
		t.Fatal(err)
	}
	wave(t, file)
	replacement, err := worker.Access.Inspect("music", "album/song.wav")
	if err != nil {
		t.Fatal(err)
	}
	rebind := pluginhost.LocalRebind{JobID: domain.NewID(), RootID: "music", RootIdentity: worker.Access.Roots()[0].ExpectedIdentity, RelativePath: entry.RelativePath, ExternalID: observations[0].ExternalID, OldIdentity: entry.FileIdentity, OldVersion: entry.ObservedVersion, NewIdentity: replacement.FileIdentity, NewVersion: replacement.ObservedVersion}
	for i := 0; i < 2; i++ {
		updated, err := worker.RebindLocalTrack(ctx, rebind)
		if err != nil || updated.ExternalID != observations[0].ExternalID || updated.SourceRef != observations[0].SourceRef || updated.ObservedVersion != replacement.ObservedVersion {
			t.Fatal("registered identity protocol did not preserve the custom provider reference", updated, err)
		}
	}
	desired := config.Config{Plugins: []config.Plugin{
		{PluginID: id, Enabled: true, Instances: []config.Instance{
			{InstanceID: instance, Enabled: true, Permissions: &config.Permissions{ReadDirectories: []string{root}},
				Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "music", Name: "Music", Path: root}}}},
		}},
	}}
	manager, err := plugincontrol.New(ctx, desired, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	configuration, err := manager.Configuration(instance)
	if err != nil || configuration.ImportOptions.SchemaRevision != "test-directory-v1" || configuration.ImportOptions.ImportDefaults.MaxDepth != 2 {
		t.Fatal("configuration lost the registered provider declaration", configuration, err)
	}
	page, err := manager.Directories(ctx, instance, "", "")
	if err != nil || len(page.Items) != 1 {
		t.Fatal("directory selection still depends on the official plugin ID", page, err)
	}
	if _, err := manager.Directories(ctx, instance, t.TempDir(), ""); err == nil {
		t.Fatal("custom provider bypassed directory grants")
	}
}

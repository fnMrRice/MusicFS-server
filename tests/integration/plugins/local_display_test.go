package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func TestLocalDisplayHelperProcess(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "local-display=") {
			mode = strings.TrimPrefix(arg, "local-display=")
		}
	}
	if mode == "" {
		return
	}
	identity := localfiles.Identity()
	identity.Name = "Fixture " + mode
	identity.LocalizedNames = map[string]string{"zh-Hans": "测试本地音乐", "fr": "Musique locale"}
	switch mode {
	case "missing":
		identity = nil
	case "wrong-id":
		identity.ID = "musicfs.plugin.source.netease"
	case "missing-feature":
		identity.Features = []string{}
	case "invalid-name":
		identity.Name = "Invalid\nName"
	}
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		var hello pluginproto.Hello
		if method != "host.hello" || json.Unmarshal(raw, &hello) != nil {
			return nil, pluginproto.Failure("UNSUPPORTED", "Unsupported fixture method")
		}
		manifest := pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", Plugin: identity, PluginVersion: "0.3.0", ProtocolVersion: pluginproto.ProtocolVersion, Roles: []string{"provider"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}
		return pluginproto.Ready{Manifest: manifest, InstanceID: hello.InstanceID, Nonce: hello.Nonce}, nil
	})
	<-peer.Done()
	os.Exit(0)
}

func localDisplayConfig(t *testing.T, mode string) pluginhost.Config {
	t.Helper()
	b, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return pluginhost.Config{Executable: os.Args[0], Args: []string{"-test.run=^TestLocalDisplayHelperProcess$", "--", "local-display=" + mode}, SHA256: fmt.Sprintf("%x", sum), InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", UserID: "owner", StateDir: t.TempDir()}
}

type displayRepository struct{ library.Repository }

func (displayRepository) ProviderScanState(context.Context, string, string) string { return "idle" }

func TestLocalMusicIdentityFlowsFromWorkerAndSurvivesStop(t *testing.T) {
	ctx := context.Background()
	start := func(mode string) *pluginhost.Worker {
		t.Helper()
		worker, err := pluginhost.Start(ctx, localDisplayConfig(t, mode))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = worker.Close() })
		return worker
	}
	w := start("first")
	runtime := library.Runtime{ID: "local", Owner: "owner", Name: "Private instance alias", Plugin: localfiles.Identity(), Worker: w}
	service := library.New(displayRepository{}, jobs.New(nil), []library.Runtime{runtime}, nil)
	read := func(want, availability string) library.Provider {
		t.Helper()
		providers := service.Providers(ctx, "owner")
		if len(providers) != 1 || providers[0].Plugin == nil || providers[0].Plugin.ID != "musicfs.plugin.source.localfiles" || providers[0].Plugin.Name != want || providers[0].Availability != availability || providers[0].Plugin.LocalizedNames["fr"] != "Musique locale" || len(providers[0].Plugin.Features) != 1 || providers[0].Plugin.Features[0] != "music-source" {
			t.Fatalf("plugin identity was not retained: %+v", providers)
		}
		return providers[0]
	}
	// Neither callers nor the public manifest may rewrite the verified cache.
	w.Manifest.Plugin.Name = "Manifest mutation"
	w.PluginIdentity().LocalizedNames["fr"] = "Worker copy mutation"
	provider := read("Fixture first", "available")
	provider.Plugin.Name = "API copy mutation"
	provider.Plugin.Features[0] = "other"
	current, _ := service.Runtime("local")
	current.Plugin.LocalizedNames["fr"] = "Runtime copy mutation"
	service.Runtimes()[0].Plugin.Name = "Runtime list mutation"
	read("Fixture first", "available")
	if providers := service.Providers(ctx, "other"); len(providers) != 0 {
		t.Fatal("plugin metadata escaped its owner scope")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.Worker = nil
	runtime.Plugin = localfiles.Identity()
	if err := service.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	read("Fixture first", "offline")
	// A failed start does not replace a previously verified identity.
	if failed, err := pluginhost.Start(ctx, localDisplayConfig(t, "wrong-id")); err == nil {
		_ = failed.Close()
		t.Fatal("mismatched plugin identity accepted")
	}
	read("Fixture first", "offline")
	runtime.Worker = start("second")
	if err := service.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	read("Fixture second", "available")
}

func TestLocalWorkerRequiresValidMusicSourceDeclaration(t *testing.T) {
	for _, mode := range []string{"missing", "wrong-id", "missing-feature", "invalid-name"} {
		t.Run(mode, func(t *testing.T) {
			if worker, err := pluginhost.Start(context.Background(), localDisplayConfig(t, mode)); err == nil {
				_ = worker.Close()
				t.Fatal("invalid music source declaration accepted")
			}
		})
	}
}

func TestNeverStartedLocalProviderDoesNotInventPluginIdentity(t *testing.T) {
	service := library.New(displayRepository{}, jobs.New(nil), []library.Runtime{{ID: "local", Owner: "owner", Name: "Private alias"}}, nil)
	providers := service.Providers(context.Background(), "owner")
	if len(providers) != 1 || providers[0].Plugin != nil {
		t.Fatal("never-started provider exposed unverified plugin metadata")
	}
	wire, err := json.Marshal(providers[0])
	if err != nil || strings.Contains(string(wire), `"plugin":`) {
		t.Fatal("never-started provider did not omit plugin identity", string(wire), err)
	}
}

func TestBundledLocalProviderKeepsItsDeclarationBeforeWorkerStarts(t *testing.T) {
	identity := localfiles.Identity()
	runtime := library.Runtime{ID: "custom-local-instance", Owner: "owner", Name: "Private alias", Plugin: identity}
	service := library.New(displayRepository{}, jobs.New(nil), []library.Runtime{runtime}, nil)
	identity.Name = "Caller mutation"
	identity.LocalizedNames["zh-Hans"] = "Caller mutation"
	read := func() {
		t.Helper()
		providers := service.Providers(context.Background(), "owner")
		if len(providers) != 1 || providers[0].Plugin == nil || providers[0].Plugin.Name != "Local Music" || providers[0].Plugin.LocalizedNames["zh-Hans"] != "本地音乐" || providers[0].Availability != "offline" || len(providers[0].Capabilities) != 0 {
			t.Fatalf("bundled plugin identity missing before worker startup: %+v", providers)
		}
		providers[0].Plugin.Name = "Response mutation"
	}
	read()
	runtime.Plugin = nil
	if err := service.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	read()
}

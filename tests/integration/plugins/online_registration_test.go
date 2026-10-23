package plugins_test

import (
	"context"
	"testing"
	"time"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
)

// 新平台通过真实 Worker 协议接入，验证通用宿主不再依赖官方平台枚举。
func TestRegisteredOnlinePlatformUsesDeclaredResourcesCapabilitiesAndLogin(t *testing.T) {
	const platform = "independent-catalog"
	descriptor := pluginproto.OnlinePolicy(platform)
	descriptor.Plugin = &pluginproto.PluginIdentity{ID: pluginproto.OnlinePluginID(platform), Name: "Independent catalog", LocalizedNames: map[string]string{}, Features: []string{"music-source"}}
	descriptor.Authentication = "anonymous"
	descriptor.Capabilities = online.DeclareCapabilities("metadata.album")
	descriptor.Login = &online.LoginDescriptor{Method: "deviceCode", Validation: "pluginConfirmed"}
	descriptor.Resources = online.ResourcePolicy{
		Albums:     []online.ReferenceRule{{IDPattern: `^release_([a-z]+)\.([0-9]+)$`, URLTemplate: `https://catalog.example/releases/${1}/${2}`}},
		ImageHosts: []string{"images.catalog.example"},
	}
	factory := func(onlineplugins.Options) provider.Plugin {
		return provider.Plugin{Descriptor: descriptor, Version: "1.0.0", NewClient: func(online.Scope, time.Duration) (provider.Client, error) { return &registeredCatalogClient{}, nil }}
	}
	if err := onlineplugins.Register(platform, factory); err != nil {
		t.Fatal(err)
	}
	if err := onlineplugins.Register(platform, factory); err == nil {
		t.Fatal("duplicate runtime registration was accepted")
	}
	if err := pluginconfig.Register(pluginconfig.Declaration{ID: descriptor.Plugin.ID, Version: "1.0.0", Role: "provider", SourceType: "network", Platform: platform, SupportsMultipleInstances: true, Identity: descriptor.Plugin}); err != nil {
		t.Fatal(err)
	}
	cfg := pluginhost.OnlineConfig{Builtin: true, StateDir: t.TempDir(), Platform: platform, UserID: "owner", InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Timeout: 3 * time.Second}
	worker := onlineStart(t, cfg)
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: cfg.UserID, InstanceID: cfg.InstanceID, Platform: platform, ConfigRevision: "1", Enabled: true, Provider: worker}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	options, err := host.AccountOptions(ctx, cfg.UserID, cfg.InstanceID)
	if err != nil || options.Method != "deviceCode" || !options.Configured || options.Validation != "pluginConfirmed" {
		t.Fatal("plugin login declaration was not honored", options, err)
	}
	album, err := host.Album(ctx, cfg.UserID, cfg.InstanceID, "release_blue.42")
	if err != nil || album.SourceURL != "https://catalog.example/releases/blue/42" || album.CoverURL == nil || *album.CoverURL != "https://images.catalog.example/cover.jpg" {
		t.Fatal("independent resource rules were not honored", album, err)
	}
	if _, err := worker.Album(ctx, "release_forged.42"); err == nil {
		t.Fatal("forged source URL was accepted")
	}
	if _, err := host.Album(ctx, "other", cfg.InstanceID, "release_blue.42"); err == nil {
		t.Fatal("custom platform bypassed owner authorization")
	}
	if _, err := worker.Metadata(ctx, "release_blue.42"); err == nil {
		t.Fatal("undeclared capability was accepted")
	}
}

type registeredCatalogClient struct{ provider.Client }

func (*registeredCatalogClient) Close() {}
func (*registeredCatalogClient) Account(context.Context, online.AccountRequest) (online.AccountResult, error) {
	return online.AccountResult{View: online.AccountView{State: "signedOut"}}, nil
}
func (*registeredCatalogClient) AccountOptions(context.Context) (online.AccountOptions, error) {
	return online.AccountOptions{Method: "deviceCode", Configured: true, Validation: "pluginConfirmed"}, nil
}
func (*registeredCatalogClient) Album(_ context.Context, id string) (online.Album, error) {
	cover := "https://images.catalog.example/cover.jpg"
	return online.Album{ExternalID: id, Title: "Independent release", SourceURL: "https://catalog.example/releases/blue/42", CoverURL: &cover, Artists: []string{"Artist"}}, nil
}

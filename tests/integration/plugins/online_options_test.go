package plugins_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"testing"
	"time"
)

func TestBuiltinLoginOptionsRemainPluginOwnedAndOwnerScoped(t *testing.T) {
	// 仅使用合成开发者令牌，不读取或输出真实凭据。
	t.Setenv("MUSICFS_APPLE_DEVELOPER_TOKEN", "synthetic-public-client-token")
	for _, tc := range []struct {
		platform, method string
		configured       bool
	}{{online.Netease, "qr", true}, {online.AppleMusic, "musicKit", true}, {online.Bilibili, "none", false}} {
		t.Run(tc.platform, func(t *testing.T) {
			cfg := pluginhost.OnlineConfig{Builtin: true, StateDir: t.TempDir(), Platform: tc.platform, UserID: "owner", InstanceID: "a82dbe7f-4bf7-5101-9317-70c7890b19d7", Timeout: 3 * time.Second}
			worker := onlineStart(t, cfg)
			host, err := onlinehost.New([]onlinehost.Runtime{{Owner: cfg.UserID, InstanceID: cfg.InstanceID, Platform: tc.platform, ConfigRevision: "1", Enabled: true, Provider: worker}})
			if err != nil {
				t.Fatal(err)
			}
			options, err := host.AccountOptions(context.Background(), cfg.UserID, cfg.InstanceID)
			if err != nil || options.Method != tc.method || options.Configured != tc.configured {
				t.Fatalf("unexpected login options: method=%s configured=%v error=%v", options.Method, options.Configured, err)
			}
			if (options.DeveloperToken != "") != (tc.platform == online.AppleMusic) {
				t.Fatal("client token crossed plugin boundary")
			}
			if tc.platform == online.Bilibili {
				wireOptions, err := worker.PlatformAccountOptions(context.Background())
				if err != nil || wireOptions.Method != "none" || wireOptions.Configured {
					t.Fatal("anonymous worker advertised login", err)
				}
				if _, err := host.Account(context.Background(), cfg.UserID, cfg.InstanceID, online.AccountRequest{Action: "begin"}); err == nil {
					t.Fatal("anonymous platform accepted login")
				}
			}
			if _, err := host.AccountOptions(context.Background(), "other", cfg.InstanceID); err == nil {
				t.Fatal("another user read login options")
			}
		})
	}
}

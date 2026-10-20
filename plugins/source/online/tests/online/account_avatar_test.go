package online_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestAccountAvatarRestorationAndRefresh(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, platform, raw, want string }{
		{"https", online.Netease, "https://p1.music.126.net/avatar.jpg", "https://p1.music.126.net/avatar.jpg"},
		{"upgrade_http", online.Netease, "http://p2.music.126.net/avatar.jpg", "https://p2.music.126.net/avatar.jpg"},
		{"third_host", online.Netease, "https://p3.music.126.net/avatar.jpg", "https://p3.music.126.net/avatar.jpg"},
		{"fourth_host", online.Netease, "https://p4.music.126.net/avatar.jpg", "https://p4.music.126.net/avatar.jpg"},
		{"absent", online.Netease, "", ""},
		{"external_host", online.Netease, "https://example.com/avatar.jpg", ""},
		{"suffix_attack", online.Netease, "https://p1.music.126.net.example.com/avatar.jpg", ""},
		{"unknown_cdn", online.Netease, "https://unverified.music.126.net/avatar.jpg", ""},
		{"credentials", online.Netease, "https://user@p1.music.126.net/avatar.jpg", ""},
		{"port", online.Netease, "https://p1.music.126.net:443/avatar.jpg", ""},
		{"query", online.Netease, "https://p1.music.126.net/avatar.jpg?token=private", ""},
		{"empty_query", online.Netease, "https://p1.music.126.net/avatar.jpg?", ""},
		{"fragment", online.Netease, "https://p1.music.126.net/avatar.jpg#token", ""},
		{"data", online.Netease, "data:image/png;base64,fixture", ""},
		{"relative", online.Netease, "//p1.music.126.net/avatar.jpg", ""},
		{"empty_path", online.Netease, "https://p1.music.126.net/", ""},
		{"oversized", online.Netease, "https://p1.music.126.net/" + strings.Repeat("a", 2048), ""},
		{"oversized_after_upgrade", online.Netease, "http://p1.music.126.net/" + strings.Repeat("a", 2026), ""},
		{"other_platform", online.QQMusic, "https://p1.music.126.net/avatar.jpg", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, _ := json.Marshal(map[string]any{"code": 200, "profile": map[string]any{"userId": 12345, "nickname": "Updated listener", "avatarUrl": tc.raw}})
			c := anonymousScoped(t, tc.platform, response(string(profile)))
			secret, _ := json.Marshal(map[string]any{"platform": tc.platform, "userId": "user", "instanceId": "instance", "accountId": "12345", "name": "Saved listener", "avatarUrl": tc.raw, "cookies": map[string]string{}})
			ctx := context.Background()
			result, err := c.Account(ctx, online.AccountRequest{Action: "restore", Secret: secret})
			if err != nil || result.View.AvatarURL != tc.want || result.View.State != "unverified" {
				t.Fatalf("Restored avatar did not match the allowed platform URL: %+v, %v", result.View, err)
			}
			if tc.platform == online.Netease {
				result, err = c.Account(ctx, online.AccountRequest{Action: "check"})
				if err != nil || result.View.AvatarURL != tc.want || result.View.DisplayName != "Updated listener" || result.View.State != "signedIn" {
					t.Fatalf("Optional avatar affected account validation: %+v, %v", result.View, err)
				}
			}
		})
	}
}

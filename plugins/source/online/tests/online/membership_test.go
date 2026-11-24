package online_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func TestNeteaseVerifiedMembership(t *testing.T) {
	for _, tc := range []struct {
		name, primary, detail, want string
	}{
		{"ordinary", `,"vipType":0`, "", "free"},
		{"generic VIP", `,"vipType":11`, "", "vip"},
		{"active redplus", `,"vipType":11,"vipRights":{"redplus":{"vipCode":300,"rights":true}}`, "", "svip"},
		{"inactive redplus", `,"vipType":11,"vipRights":{"redplus":{"vipCode":300,"rights":false}}`, "", "vip"},
		{"wrong entitlement", `,"vipType":11,"vipRights":{"redplus":{"vipCode":100,"rights":true}}`, "", "vip"},
		{"unrecognized entitlement shape", `,"vipType":11,"vipRights":"other"`, "", "vip"},
		{"unknown upstream tier", `,"vipType":42`, "", ""},
		{"malformed upstream tier", `,"vipType":"11"`, "", ""},
		{"missing tier uses own public profile", "", `{"code":200,"profile":{"userId":12345,"vipType":11}}`, "vip"},
		{"other profile", "", `{"code":200,"profile":{"userId":555,"vipType":11}}`, ""},
		{"upstream rejects optional profile", `,"vipType":null`, `{"code":301}`, ""},
		{"missing optional tier", "", `{"code":200,"profile":{"userId":12345,"redVipLevel":7}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			c := anonymousScoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				switch r.URL.Path {
				case "/eapi/nuser/account/get":
					return accountResponse(fmt.Sprintf(`{"code":200,"profile":{"userId":12345,"nickname":"Listener"%s}}`, tc.primary)), nil
				case "/api/v1/user/detail/12345":
					if tc.detail == "" || r.Method != http.MethodGet || r.URL.Host != "interface.music.163.com" || r.Header.Get("Cookie") != "" {
						t.Fatal("unexpected public profile request")
					}
					return accountResponse(tc.detail), nil
				default:
					t.Fatal("unexpected endpoint", r.URL.Path)
					return nil, nil
				}
			}))
			ctx := t.Context()
			secret := []byte(`{"platform":"netease","userId":"user","instanceId":"instance","deviceId":"test","accountId":"12345","cookies":{"MUSIC_U":"fixture"},"membershipTier":"svip"}`)
			restored, err := c.Account(ctx, online.AccountRequest{Action: "restore", Secret: secret})
			if err != nil || restored.View.MembershipTier != "" || calls != 0 {
				t.Fatal("restored unverified account exposed membership", err)
			}
			for _, action := range []string{"check", "state", "state"} {
				v, err := c.Account(ctx, online.AccountRequest{Action: action})
				if err != nil || v.View.State != "signedIn" || v.View.MembershipTier != tc.want {
					t.Fatalf("%s returned %q, %q, %v", action, v.View.State, v.View.MembershipTier, err)
				}
			}
			wantCalls := 1
			if tc.detail != "" {
				wantCalls++
			}
			if calls != wantCalls {
				t.Fatalf("expected %d upstream calls, got %d", wantCalls, calls)
			}
		})
	}
}

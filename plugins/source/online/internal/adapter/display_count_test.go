package adapter

import (
	"context"
	"testing"
	"time"
)

func TestAccountResultOwnsCountAndUnitWithoutUsingNickname(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name string
		view AccountView
		want int64
	}{
		{"verified unnamed account", AccountView{State: "signedIn", AccountID: "123", Revision: "v1"}, 1},
		{"signed out", AccountView{State: "signedOut"}, 0},
		{"unverified stored account", AccountView{State: "unverified", AccountID: "123", DisplayName: "Known user"}, 0},
		{"missing identity", AccountView{State: "signedIn", Revision: "v1", DisplayName: "Known user"}, 0},
		{"expired identity", AccountView{State: "signedIn", AccountID: "123", Revision: "v1", ExpiresAt: &past}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{account: accountState{view: tc.view}}
			result, err := client.Account(context.Background(), AccountRequest{Action: "state"})
			if err != nil || result.DisplayCount == nil || result.DisplayCount.Validate() != nil || result.DisplayCount.Value != tc.want || result.DisplayCount.Unit != "users" || result.DisplayCount.LocalizedUnits["zh-Hans"] != "个用户" {
				t.Fatalf("unexpected plugin statistic: %#v, %v", result.DisplayCount, err)
			}
			result, err = client.Account(context.Background(), AccountRequest{Action: "logout"})
			if err != nil || result.DisplayCount == nil || result.DisplayCount.Value != 0 {
				t.Fatal("logout did not return the plugin's zero statistic", err)
			}
		})
	}
}

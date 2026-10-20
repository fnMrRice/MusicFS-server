package online_test

import (
	"context"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"os"
	"testing"
	"time"
)

// Explicit probe obtains and cancels a challenge; it never logs a user in,
// prints a login URL, or records credentials in a fixture.
func TestPlatformLoginLiveChallenge(t *testing.T) {
	if os.Getenv("MUSICFS_LIVE_LOGIN_PROBE") != "1" {
		t.Skip("Live login challenge probe is disabled")
	}
	for _, platform := range []string{online.Netease, online.QQMusic} {
		t.Run(platform, func(t *testing.T) {
			c, err := onlineplugins.New(platform, online.Scope{UserID: "probe", InstanceID: "probe"}, online.Options{Timeout: 15 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := c.Account(ctx, online.AccountRequest{Action: "begin"})
			if err != nil {
				t.Fatal(err)
			}
			if result.View.LoginID == "" || result.View.Method != "qr" {
				t.Fatal("No usable QR challenge was returned")
			}
			if _, err = c.Account(ctx, online.AccountRequest{Action: "cancel", LoginID: result.View.LoginID}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

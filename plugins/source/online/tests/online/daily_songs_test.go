package online_test

import (
	"context"
	"testing"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestDailySongsValidatePlatformResponse(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"empty", `{"code":200,"data":{"dailySongs":[]}}`, ""},
		{"missing list", `{"code":200,"data":{}}`, "TEMPORARY_FAILURE"},
		{"missing status", `{"data":{"dailySongs":[]}}`, "TEMPORARY_FAILURE"},
		{"invalid track", `{"code":200,"data":{"dailySongs":[{"name":"Unknown"}]}}`, "TEMPORARY_FAILURE"},
		{"expired account", `{"code":301}`, "PLATFORM_AUTH_REQUIRED"},
		{"rate limit", `{"code":429}`, "RATE_LIMITED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, response(tc.body))
			page, err := c.DailySongs(context.Background(), 20)
			if tc.code != "" {
				expectCode(t, err, tc.code)
			} else if err != nil || page.Tracks == nil || len(page.Tracks) != 0 || page.ObservedAt.IsZero() {
				t.Fatal("invalid empty daily song page", page, err)
			}
		})
	}
}

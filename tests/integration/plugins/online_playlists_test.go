package plugins_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func TestAccountPlaylistsCrossWorkerBoundary(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "account-playlists")
	w := onlineStart(t, onlineConfig(t, path, online.Netease))
	request := online.PlaylistsRequest{Limit: 20}
	_, err := w.Playlists(context.Background(), request)
	onlineExpect(t, err, "PLATFORM_AUTH_REQUIRED")
	loginOnlineFixture(t, w)
	page, err := w.Playlists(context.Background(), request)
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "91" || page.Items[0].Title != "Account playlist" || page.NextCursor != nil {
		t.Fatal("account playlist round trip failed", page, err)
	}
	if _, err := w.PlatformAccount(context.Background(), online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	_, err = w.Playlists(context.Background(), request)
	onlineExpect(t, err, "PLATFORM_AUTH_REQUIRED")
}

package e2e_test

import (
	"os"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

// Live checks are explicitly opt-in. They never send platform credentials or
// download audio; only one public result per query is requested.
func TestOnlineLiveAnonymousHTTP(t *testing.T) {
	if os.Getenv("MUSICFS_ONLINE_LIVE") != "1" {
		t.Skip("explicit public-network validation not enabled")
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{onlinePlugin(online.Netease, build(t, "musicfs-plugin-netease"), "net"), onlinePlugin(online.QQMusic, build(t, "musicfs-plugin-qqmusic"), "qq"), onlinePlugin(online.AppleMusic, build(t, "musicfs-plugin-applemusic"), "apple")}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	page := albumRequest[online.SearchPage](t, c, "GET", "/online-instances/net/search?q=MusicFS&limit=1", nil, "", 200, "OnlineSearchPage")
	if len(page.Tracks) != 1 {
		t.Fatal("live search did not return a usable public sample")
	}
	time.Sleep(1100 * time.Millisecond)
	track := albumRequest[online.Track](t, c, "GET", "/online-instances/net/tracks/"+page.Tracks[0].ExternalID, nil, "", 200, "OnlineTrack")
	if track.ExternalID != page.Tracks[0].ExternalID || track.PlaybackAvailability != "unknown" {
		t.Fatal("live metadata identity mismatch")
	}
	time.Sleep(1100 * time.Millisecond)
	recommendations := albumRequest[online.RecommendationPage](t, c, "GET", "/online-instances/net/recommendations?limit=1", nil, "", 200, "OnlineRecommendationPage")
	if len(recommendations.Items) != 1 || recommendations.Personalized {
		t.Fatal("live recommendation did not return a public sample")
	}
	time.Sleep(1100 * time.Millisecond)
	observation := albumRequest[online.PlaylistObservation](t, c, "GET", "/online-instances/net/playlists/"+recommendations.Items[0].Playlist.ExternalID, nil, "", 200, "OnlineObservation")
	t.Logf("Netease HTTP: search=%d metadataIdentity=true recommendations=%d entries=%d complete=%t consistency=%s", len(page.Tracks), len(recommendations.Items), len(observation.Entries), observation.Complete, observation.Consistency)
	code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/qq/search?q=MusicFS&limit=1", nil, "")
	if code == 200 {
		testkit.ValidateHTTP(t, "OnlineSearchPage", raw)
	} else {
		testkit.ValidateHTTP(t, "Error", raw)
	}
	t.Logf("QQ HTTP status=%d; support declaration remains unverified", code)
	code, raw, _ = c.request(t, "GET", "/api/v1/online-instances/apple/search?q=MusicFS&limit=1", nil, "")
	if code != 401 {
		t.Fatalf("Apple authorization boundary: %d", code)
	}
	testkit.ValidateHTTP(t, "Error", raw)
	for _, id := range []string{"net", "qq", "apple"} {
		code, _, _ := c.request(t, "POST", "/api/v1/online-instances/"+id+"/tracks/42/resolve", struct{}{}, "")
		if code != 415 {
			t.Fatal("unverified audio resource was resolved")
		}
	}
	assertOnlineDoesNotImport(t, c)
}

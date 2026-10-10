package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func onlinePlugin(platform, program string, ids ...string) config.Plugin {
	instances := []config.Instance{}
	for _, id := range ids {
		instances = append(instances, config.Instance{InstanceID: id, Role: "provider", Enabled: true})
	}
	return config.Plugin{PluginID: "musicfs." + platform, Executable: program, Enabled: true, Instances: instances}
}

func TestOnlineActualServerAnonymousWorkerAndDisabledRestart(t *testing.T) {
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	dir := t.TempDir()
	fixture := filepath.Join(dir, "netfixture"+suffix)
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", fixture, "./tests/testkit/online-worker")
	cmd.Dir = testkit.RepositoryRoot()
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build online fixture: %v %s", err, raw)
	}
	qqFixture := filepath.Join(dir, "qqfixture"+suffix)
	body, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(qqFixture, body, 0700); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{onlinePlugin(online.Netease, fixture, "net", "net2"), onlinePlugin(online.QQMusic, qqFixture, "qq"), onlinePlugin(online.AppleMusic, build(t, "musicfs-plugin-applemusic"), "apple")}
	cfg.Plugins[0].Instances = append(cfg.Plugins[0].Instances, config.Instance{InstanceID: "disabled", Role: "provider"})
	t.Run("read-only-public-scope", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		instances := albumRequest[struct {
			Items []onlinehost.Instance `json:"items"`
		}](t, c, "GET", "/online-instances", nil, "", 200, "OnlineInstances")
		if len(instances.Items) != 5 {
			t.Fatal("configured online instances missing")
		}
		for _, instance := range instances.Items {
			want := "available"
			if instance.InstanceID == "disabled" {
				want = "offline"
			}
			if instance.Availability != want || instance.AccountID != "" {
				t.Fatalf("incorrect runtime availability: %+v", instance)
			}
		}
		page := albumRequest[online.SearchPage](t, c, "GET", "/online-instances/net/search?q=Synthetic&limit=1", nil, "", 200, "OnlineSearchPage")
		if len(page.Tracks) != 1 || page.Tracks[0].ExternalID != "42" {
			t.Fatal("real worker metadata was not delivered")
		}
		for _, tc := range []struct {
			path   string
			status int
		}{{"net/search?q=Synthetic&limit=1", 429}, {"disabled/search?q=Synthetic", 503}, {"apple/search?q=Synthetic", 401}, {"qq/search?q=Synthetic", 503}} {
			code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/"+tc.path, nil, "")
			if code != tc.status {
				t.Fatalf("bounded platform failure %s: %d %s", tc.path, code, raw)
			}
			testkit.ValidateHTTP(t, "Error", raw)
		}
		time.Sleep(1050 * time.Millisecond)
		track := albumRequest[online.Track](t, c, "GET", "/online-instances/net/tracks/42", nil, "", 200, "OnlineTrack")
		if track.PlaybackAvailability != "unknown" || track.MetadataDigest == "" {
			t.Fatal("metadata was incorrectly advertised as audio")
		}
		time.Sleep(1050 * time.Millisecond)
		recommendations := albumRequest[online.RecommendationPage](t, c, "GET", "/online-instances/net/recommendations?limit=1", nil, "", 200, "OnlineRecommendationPage")
		if recommendations.Personalized || recommendations.NextCursor != nil {
			t.Fatal("anonymous recommendations falsely personalized")
		}
		time.Sleep(1050 * time.Millisecond)
		observation := albumRequest[online.PlaylistObservation](t, c, "GET", "/online-instances/net/playlists/91", nil, "", 200, "OnlineObservation")
		if !observation.Complete || observation.Consistency != online.Unversioned || len(observation.Entries) != 3 || observation.Entries[0].ExternalTrackID != observation.Entries[1].ExternalTrackID {
			t.Fatal("playlist occurrences or observation semantics changed")
		}
		for _, id := range []string{"net", "qq", "apple"} {
			code, raw, _ := c.request(t, "POST", "/api/v1/online-instances/"+id+"/tracks/42/resolve", struct{}{}, "")
			if code != 415 {
				t.Fatalf("unsupported audio resolve: %d %s", code, raw)
			}
		}
		assertOnlineDoesNotImport(t, c)
	})
	t.Run("disabled-restart", func(t *testing.T) {
		for i := range cfg.Plugins {
			cfg.Plugins[i].Enabled = false
		}
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/net/search?q=Synthetic", nil, "")
		if code != 503 {
			t.Fatalf("disabled provider was used: %d %s", code, raw)
		}
		assertOnlineDoesNotImport(t, c)
	})
}

func assertOnlineDoesNotImport(t *testing.T, c *client) {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 0 {
		t.Fatalf("public observation implicitly imported sources: %d %s", code, raw)
	}
}

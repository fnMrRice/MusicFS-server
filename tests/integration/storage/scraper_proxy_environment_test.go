package storage_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func TestScraperProxyServerEnvironment(t *testing.T) {
	// Go 会缓存进程环境代理；子进程确保此测试独立验证默认行为和 NO_PROXY。
	if os.Getenv("MUSICFS_TEST_SCRAPER_PROXY_ENV") == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestScraperProxyServerEnvironment$", "-test.v")
		cmd.Env = append(os.Environ(), "MUSICFS_TEST_SCRAPER_PROXY_ENV=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("environment proxy subprocess failed: %v\n%s", err, output)
		}
		return
	}
	network := newScraperProxyNetwork(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "musicbrainz.org" {
			fmt.Fprintf(w, `{"releases":[{"id":"%s","title":"Synthetic album"}]}`, proxyReleaseID)
			return
		}
		proxyLyrics(w, r)
	})
	t.Setenv("HTTPS_PROXY", network.httpURL)
	t.Setenv("HTTP_PROXY", network.httpURL)
	t.Setenv("NO_PROXY", "musicbrainz.org")
	service, configuration := newProxyServices(t, evidenceStore(t), network.client)
	const id = "musicfs.scraper.lrclib"
	if _, err := service.Search(t.Context(), "admin", id, "lyrics", scraper.Query{ExternalID: "1"}); err != nil {
		t.Fatal(err)
	}
	if network.count("http", "lrclib.net:443") != 1 {
		t.Fatal("default scraper ignored HTTPS_PROXY")
	}
	saveProxy(t, configuration, "admin", id, "direct", "")
	if _, err := service.Search(t.Context(), "admin", id, "lyrics", scraper.Query{ExternalID: "1"}); err != nil {
		t.Fatal(err)
	}
	if network.count("direct", "lrclib.net:443") != 1 || network.count("http", "lrclib.net:443") != 1 {
		t.Fatal("direct mode inherited server proxy")
	}
	if _, err := service.Search(t.Context(), "admin", "musicfs.scraper.musicbrainz", "albumDetails", scraper.Query{Album: "Synthetic album"}); err != nil {
		t.Fatal(err)
	}
	if network.count("direct", "musicbrainz.org:443") != 1 || network.count("http", "musicbrainz.org:443") != 0 {
		t.Fatal("environment mode ignored NO_PROXY")
	}
}

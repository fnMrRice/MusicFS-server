package e2e_test

import (
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

func TestOnlinePlaylistDownloadWithExplicitQuality(t *testing.T) {
	const instance = "7fe223d0-a3bd-5e87-87f8-305dc26d9041"
	const basePath = "/online-instances/" + instance
	cfg, base := yamlServiceConfig(t)
	plugin := onlinePlugin(online.Netease, managedOnlineFixture(t), instance)
	plugin.Args = []string{"playlist-download"}
	cfg.Plugins = []config.Plugin{plugin}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	account := authenticatePlaylistFixture(t, c)
	options := albumRequest[pluginproto.QualityOptions](t, c, "GET", basePath+"/download-qualities", nil, "", 200, "OnlineDownloadQualities")
	if len(options.Options) != 7 || !options.Supports("lossless") || options.DefaultQuality != "exhigh" {
		t.Fatalf("unexpected NetEase download options: %+v", options)
	}
	type item struct {
		ExternalID, State string
		Version           *onlinecache.Version
		Error             *struct{ Code string }
	}
	type job struct {
		JobID, State      string
		Completed, Failed int
		Items             []item
	}
	makeJob := func(ids []string, quality, key string) job {
		t.Helper()
		return albumRequest[job](t, c, "POST", basePath+"/download-jobs", map[string]any{"accountId": account, "quality": quality, "externalIds": ids}, key, 202, "OnlineDownloadJob")
	}
	read := func(id string) job {
		t.Helper()
		return albumRequest[job](t, c, "GET", "/online-download-jobs/"+id, nil, "", 200, "OnlineDownloadJob")
	}
	wait := func(j job) job {
		t.Helper()
		deadline := time.Now().Add(45 * time.Second)
		for j.State == "queued" || j.State == "running" {
			if time.Now().After(deadline) {
				t.Fatalf("download job did not finish: %+v", j)
			}
			time.Sleep(150 * time.Millisecond)
			j = read(j.JobID)
		}
		return j
	}
	ids := []string{"42", "99", "43", "42"}
	batch := wait(makeJob(ids, "lossless", "download-batch-fixture"))
	if batch.State != "partial" || batch.Completed != 2 || batch.Failed != 1 || len(batch.Items) != 3 ||
		batch.Items[0].Version == nil || batch.Items[0].Version.Quality != "lossless" ||
		batch.Items[1].Error == nil || batch.Items[1].Error.Code != "NOT_FOUND" ||
		batch.Items[2].Version == nil || batch.Items[2].Version.Quality != "standard" {
		t.Fatalf("unexpected batch results: %+v", batch)
	}
	if replay := makeJob(ids, "lossless", "download-batch-fixture"); replay.JobID != batch.JobID {
		t.Fatal("duplicate job on replay")
	}
	single := wait(makeJob([]string{"42"}, "standard", "download-second-quality"))
	if single.Completed != 1 || single.Items[0].Version == nil || single.Items[0].Version.ID == batch.Items[0].Version.ID || single.Items[0].Version.Quality != "standard" {
		t.Fatalf("requested version missing: %+v", single)
	}
	pending := makeJob([]string{"44"}, "lossless", "download-interrupted")
	startDeadline := time.Now().Add(20 * time.Second)
	for pending.State == "queued" {
		if time.Now().After(startDeadline) {
			t.Fatalf("download stayed queued: %+v", pending)
		}
		time.Sleep(50 * time.Millisecond)
		pending = read(pending.JobID)
	}
	if pending.State != "running" {
		t.Fatalf("expected an active download before shutdown: %+v", pending)
	}
	stop()
	startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	if resumed := wait(read(pending.JobID)); resumed.Completed != 1 || resumed.Items[0].Version == nil || resumed.Items[0].Version.Quality != "lossless" {
		t.Fatalf("unfinished download did not resume with the original login: %+v", resumed)
	}
	if restored := read(batch.JobID); restored.State != batch.State || restored.Completed != 2 {
		t.Fatalf("download results lost on restart: %+v", restored)
	}
	summary := albumRequest[job](t, c, "GET", "/online-download-jobs/"+batch.JobID+"?view=summary", nil, "", 200, "OnlineDownloadSummary")
	if summary.JobID != batch.JobID || summary.State != batch.State || summary.Completed != 2 || summary.Failed != 1 || summary.Items != nil {
		t.Fatalf("invalid download summary: %+v", summary)
	}
}

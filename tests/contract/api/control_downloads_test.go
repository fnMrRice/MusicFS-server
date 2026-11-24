package api_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/downloadjobs"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestControlDownloadProgressOnlyReachesOwner(t *testing.T) {
	repo := &memoryAuth{sessions: map[string]auth.Session{}}
	server := api.New(api.Options{Auth: auth.New(repo, testToken, []string{origin}, time.Hour)})
	gate := make(chan struct{})
	fixture := downloadFixture{save: func(ctx context.Context, _, _ string) (onlinecache.Version, error) {
		select {
		case <-gate:
			return onlinecache.Version{}, domain.Fail("AUDIO_CACHE_DISABLED", "Storage disabled")
		case <-ctx.Done():
			return onlinecache.Version{}, ctx.Err()
		}
	}}
	service, err := downloadjobs.Open(t.Context(), t.TempDir(), fixture, fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server.RegisterOnlineDownloadJobs(service)
	host := controlHTTP(t, server, nil)
	owner := connectControlUser(t, host, repo, "admin", "user")
	other := connectControlUser(t, host, repo, "other", "admin")
	job, err := service.Submit(t.Context(), "admin", "netease", "batch-download", downloadjobs.Request{ExternalIDs: []string{"a", "b"}, Quality: "lossless"})
	if err != nil {
		t.Fatal(err)
	}
	read := func(state string, failed int) {
		t.Helper()
		msg := readControl(t, owner, "event")
		var data struct {
			Jobs        []downloadjobs.Summary
			Invalidated []string
		}
		if msg.Event != "downloads.progress" || json.Unmarshal(msg.Data, &data) != nil || len(data.Jobs) != 1 || len(data.Invalidated) != 0 {
			t.Fatalf("unexpected event: %s %s", msg.Event, msg.Data)
		}
		raw, _ := json.Marshal(map[string]any{"type": "event", "event": msg.Event, "data": msg.Data})
		if err := controlSchema(t, "Event").Validate(testkit.DecodeJSON(t, raw)); err != nil {
			t.Fatal(err)
		}
		if got := data.Jobs[0]; got.ID != job.ID || got.State != state || got.Failed != failed || got.Total != 2 {
			t.Fatalf("unexpected progress: %+v", got)
		}
	}
	read("running", 0)
	close(gate)
	read("failed", 2)
	server.NotifyPluginJobs("other")
	if msg := readControl(t, other, "event"); msg.Event != "activity.changed" {
		t.Fatalf("another administrator received private download progress: %s", msg.Data)
	}
}

package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
)

func TestConfiguredOnlineOwnersStaySeparate(t *testing.T) {
	const firstID = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const secondID = "ef74c8ed-9cce-491d-9075-9a80e180884d"
	c := config.Config{Plugins: []config.Plugin{{PluginID: "musicfs.plugin.source.netease", Instances: []config.Instance{
		{InstanceID: firstID}, {InstanceID: secondID, OwnerUserID: "other-user"},
	}}}}
	service, runtimes, closeAll, err := configureOnline(context.Background(), c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	if runtimes[0].Owner != "admin" || runtimes[1].Owner != "other-user" {
		t.Fatal("configured owner was ignored", runtimes)
	}
	for _, item := range []struct{ owner, id string }{{"admin", firstID}, {"other-user", secondID}} {
		instances, err := service.Instances(context.Background(), item.owner)
		if err != nil || len(instances) != 1 || instances[0].InstanceID != item.id {
			t.Fatal("online instance list crossed user boundary", instances, err)
		}
	}
	c.Plugins[0].Instances[0].OwnerUserID = "other-user"
	_, changed, closeChanged, err := configureOnline(context.Background(), c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer closeChanged()
	if runtimes[0].ConfigRevision == changed[0].ConfigRevision {
		t.Fatal("changing account owner retained the old runtime revision")
	}
}

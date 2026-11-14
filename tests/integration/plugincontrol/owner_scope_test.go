package plugincontrol_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPluginManagementOwnerScopeAndAdministratorAllUsers(t *testing.T) {
	const pluginID = "musicfs.plugin.source.netease"
	const ownID = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const secondID = "ef74c8ed-9cce-491d-9075-9a80e180884d"
	const otherID = "720f3da0-a1ec-465f-b3f4-d2193c5adfb3"
	manager, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{
		{PluginID: pluginID, Instances: []config.Instance{
			{InstanceID: ownID},
			{InstanceID: secondID, OwnerUserID: "admin"},
			{InstanceID: otherID, OwnerUserID: "other-admin"},
		}},
		{PluginID: "musicfs.plugin.decoder.ncm", Instances: []config.Instance{{InstanceID: ncmInstanceID}}},
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	for _, item := range []struct {
		id, owner, account string
		count              int64
	}{
		{ownID, "admin", "First account", 7},
		{secondID, "admin", "Second account", 3},
		{otherID, "other-admin", "Other private account", 11},
	} {
		if err := manager.Register(item.id, plugincontrol.Hooks{AccountName: func(_ context.Context, owner string) string {
			if owner == item.owner {
				return item.account
			}
			return ""
		}, DisplayCount: func() *plugindisplay.Count {
			return &plugindisplay.Count{Value: item.count, Unit: "listeners", LocalizedUnits: map[string]string{"zh": "位听众"}}
		}}, false); err != nil {
			t.Fatal(err)
		}
	}
	server, cookies := managementHTTP(t, manager)
	for _, tc := range []struct {
		name, user, query string
		ids               []string
		count             int64
	}{
		{"default", "admin", "", []string{ownID, secondID}, 10},
		{"explicit-own", "admin", "?allUsers=false", []string{ownID, secondID}, 10},
		{"all-users", "admin", "?allUsers=true", []string{ownID, secondID, otherID}, 21},
		{"other-admin-own", "other-admin", "", []string{otherID}, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := managementRequest(server, cookies[tc.user], "GET", "/admin/plugins"+tc.query, "", "", "")
			requireHTTP(t, w, 200, "")
			testkit.ValidateHTTP(t, "PluginManagementSnapshot", w.Body.Bytes())
			var snapshot plugincontrol.Snapshot
			if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Items) < 2 || len(snapshot.Items[0].Instances) != len(tc.ids) || len(snapshot.Items[1].Instances) != 1 {
				t.Fatal("online scope or shared instances changed", w.Body.String())
			}
			count := snapshot.Items[0].DisplayCount
			if count == nil || count.Value != tc.count || count.Unit != "listeners" || count.LocalizedUnits["zh"] != "位听众" || snapshot.Items[1].DisplayCount != nil {
				t.Fatal("plugin display count did not follow the authorized scope", w.Body.String())
			}
			for index, id := range tc.ids {
				item := snapshot.Items[0].Instances[index]
				if item.InstanceID != id || item.OwnerUserID == "" || item.AccountName == "" {
					t.Fatal("instance owner or account projection is missing", item)
				}
			}
			if snapshot.Items[1].Instances[0].OwnerUserID != "" || snapshot.Items[1].Instances[0].AccountName != "" {
				t.Fatal("shared instance received account ownership")
			}
			if tc.user == "admin" && tc.query != "?allUsers=true" && strings.Contains(w.Body.String(), "Other private account") {
				t.Fatal("default snapshot leaked another owner's account")
			}
			w = managementRequest(server, cookies[tc.user], "GET", "/admin/plugins/"+pluginID+"/instances"+tc.query, "", "", "")
			requireHTTP(t, w, 200, "")
			testkit.ValidateHTTP(t, "ManagedPluginInstances", w.Body.Bytes())
			var instances struct{ Items []plugincontrol.Instance }
			if json.Unmarshal(w.Body.Bytes(), &instances) != nil || len(instances.Items) != len(tc.ids) {
				t.Fatal("plugin-specific endpoint used a different scope", w.Body.String())
			}
		})
	}
	for _, query := range []string{"?allUsers=", "?allUsers=1", "?allUsers=True", "?allUsers=false&allUsers=true", "?ownerUserId=other-admin"} {
		requireHTTP(t, managementRequest(server, cookies["admin"], "GET", "/admin/plugins"+query, "", "", ""), 400, "INVALID_ARGUMENT")
	}
	requireHTTP(t, managementRequest(server, cookies["user"], "GET", "/admin/plugins?allUsers=true", "", "", ""), 403, "ACCESS_DENIED")
	requireHTTP(t, managementRequest(server, nil, "GET", "/admin/plugins?allUsers=true", "", "", ""), 401, "AUTH_REQUIRED")
}

package plugincontrol_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func TestDisplayCountUsesOnlyVisiblePluginValues(t *testing.T) {
	const aliceID = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const bobID = "3bdd4699-d687-43f4-a96f-1ff50d5b2ccf"
	manager := displayCountManager(t, []config.Instance{
		{InstanceID: aliceID, OwnerUserID: "alice"},
		{InstanceID: bobID, OwnerUserID: "bob"},
	})
	alice := displayCount(7, "members", "位听众")
	bob := displayCount(9, "members", "位听众")
	calls := map[string]int{}
	for id, count := range map[string]*plugindisplay.Count{aliceID: alice, bobID: bob} {
		if err := manager.Register(id, plugincontrol.Hooks{DisplayCount: func() *plugindisplay.Count {
			// Reading plugin data must allow reentry into the manager.
			if manager.Gate(id) == nil {
				t.Error("missing runtime")
			}
			calls[id]++
			return count
		}}, false); err != nil {
			t.Fatal(err)
		}
	}
	for _, unscoped := range []plugincontrol.Snapshot{manager.Snapshot(), manager.SnapshotFor(context.Background(), "alice")} {
		if unscoped.Items[0].DisplayCount != nil {
			t.Fatal("unscoped snapshot exposed a count")
		}
	}
	visible := manager.SnapshotForScope(context.Background(), "alice", false).Items[0]
	if len(visible.Instances) != 1 || visible.DisplayCount == nil || visible.DisplayCount.Value != 7 || visible.DisplayCount.Unit != "members" || visible.DisplayCount.LocalizedUnits["zh"] != "位听众" {
		t.Fatal("snapshot did not preserve visible plugin data", visible)
	}
	if calls[aliceID] != 1 || calls[bobID] != 0 {
		t.Fatal("snapshot read a hidden runtime count", calls)
	}
	visible.DisplayCount.LocalizedUnits["zh"] = "API mutation"
	visible.DisplayCount.Value = 99
	if alice.Value != 7 || alice.LocalizedUnits["zh"] != "位听众" {
		t.Fatal("snapshot aliases the plugin count")
	}
	all := manager.SnapshotForScope(context.Background(), "alice", true).Items[0]
	if all.DisplayCount == nil || all.DisplayCount.Value != 16 {
		t.Fatal("authorized all-user count did not aggregate plugin values", all)
	}
	empty := manager.SnapshotForScope(context.Background(), "nobody", false).Items[0]
	if len(empty.Instances) != 0 || empty.DisplayCount != nil {
		t.Fatal("empty scope invented a count", empty)
	}
	bob.Value = -1
	if got := manager.SnapshotForScope(context.Background(), "alice", false).Items[0].DisplayCount; got == nil || got.Value != 7 {
		t.Fatal("hidden invalid count affected the visible user's total", got)
	}
}

func TestDisplayCountRequiresCompleteConsistentPluginData(t *testing.T) {
	const firstID = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const secondID = "3bdd4699-d687-43f4-a96f-1ff50d5b2ccf"
	for _, test := range []struct {
		name       string
		first      *plugindisplay.Count
		second     *plugindisplay.Count
		secondHook bool
		want       *int64
	}{
		{name: "plugin reported zero", first: displayCount(0, "users", "个用户"), second: displayCount(0, "users", "个用户"), secondHook: true, want: int64Pointer(0)},
		{name: "complete", first: displayCount(2, "users", "个用户"), second: displayCount(3, "users", "个用户"), secondHook: true, want: int64Pointer(5)},
		{name: "missing hook", first: displayCount(2, "users", "个用户")},
		{name: "missing value", first: displayCount(2, "users", "个用户"), secondHook: true},
		{name: "negative value", first: displayCount(-1, "users", "个用户"), second: displayCount(3, "users", "个用户"), secondHook: true},
		{name: "invalid unit", first: displayCount(2, "", "个用户"), second: displayCount(3, "users", "个用户"), secondHook: true},
		{name: "different unit", first: displayCount(2, "users", "个用户"), second: displayCount(3, "sources", "个来源"), secondHook: true},
		{name: "different translations", first: displayCount(2, "users", "个用户"), second: displayCount(3, "users", "位用户"), secondHook: true},
		{name: "unsafe integer sum", first: displayCount(plugindisplay.MaxValue, "users", "个用户"), second: displayCount(1, "users", "个用户"), secondHook: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := displayCountManager(t, []config.Instance{{InstanceID: firstID}, {InstanceID: secondID}})
			if err := manager.Register(firstID, plugincontrol.Hooks{DisplayCount: func() *plugindisplay.Count { return test.first }}, false); err != nil {
				t.Fatal(err)
			}
			if test.secondHook {
				if err := manager.Register(secondID, plugincontrol.Hooks{DisplayCount: func() *plugindisplay.Count { return test.second }}, false); err != nil {
					t.Fatal(err)
				}
			}
			item := manager.SnapshotForScope(context.Background(), "admin", false).Items[0]
			if test.want == nil {
				if item.DisplayCount != nil {
					t.Fatal("invalid or incomplete data produced a total", item.DisplayCount)
				}
				encoded, err := json.Marshal(item)
				if err != nil || strings.Contains(string(encoded), "displayCount") {
					t.Fatal("unavailable count was not omitted from JSON", string(encoded), err)
				}
			} else if item.DisplayCount == nil || item.DisplayCount.Value != *test.want {
				t.Fatal("unexpected count", item.DisplayCount)
			}
		})
	}
}

func TestDisplayCountIncludesSharedLocalSource(t *testing.T) {
	const id = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	manager, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{{
		PluginID: "musicfs.plugin.source.localfiles", Instances: []config.Instance{{InstanceID: id, Permissions: &config.Permissions{ReadDirectories: []string{"first", "second"}}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "selected", Name: "Selected", Path: "first"}}}}},
	}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.Register(id, plugincontrol.Hooks{DisplayCount: func() *plugindisplay.Count {
		t.Fatal("permission summary queried worker selected-root statistics")
		return nil
	}}, false); err != nil {
		t.Fatal(err)
	}
	count := manager.SnapshotForScope(context.Background(), "another-user", false).Items[0].DisplayCount
	if count == nil || count.Value != 2 || count.Unit != "allowed directories" || count.LocalizedUnits["zh-Hans"] != "个允许访问的目录" {
		t.Fatal("shared plugin count was incorrectly filtered", count)
	}
}

func displayCountManager(t *testing.T, instances []config.Instance) *plugincontrol.Manager {
	t.Helper()
	manager, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{{
		PluginID: "musicfs.plugin.source.netease", Instances: instances,
	}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func displayCount(value int64, unit, translated string) *plugindisplay.Count {
	return &plugindisplay.Count{Value: value, Unit: unit, LocalizedUnits: map[string]string{"zh": translated}}
}

func int64Pointer(value int64) *int64 { return &value }

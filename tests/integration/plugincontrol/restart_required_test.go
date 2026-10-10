package plugincontrol_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
)

func TestEnableDoesNotApplyPendingDisableAtTheOtherLevel(t *testing.T) {
	for _, initiallyDisabled := range []string{"plugin", "instance"} {
		t.Run(initiallyDisabled, func(t *testing.T) {
			raw := document
			pendingTarget, enableTarget := "", "ncm"
			if initiallyDisabled == "plugin" {
				raw = strings.Replace(raw, "    enabled: true", "    enabled: false", 1)
				pendingTarget, enableTarget = "ncm", ""
			} else {
				raw = strings.Replace(raw, "        enabled: true", "        enabled: false", 1)
			}
			h := setupDocument(t, raw)
			waitApplied(t, h.manager)
			disabled := stateChange(t, h, pendingTarget, "other-level-disable", false, "force")
			waitStatus(t, h, disabled, "succeeded")
			enabled := stateChange(t, h, enableTarget, "enable-current-level", true, "drain")
			waitStatus(t, h, enabled, "succeeded")
			snapshot := h.manager.Snapshot()
			state := snapshot.Items[0].Instances[0]
			if !snapshot.RestartRequired || !enabled.RestartRequired || state.RuntimeState != "ready" || !state.EffectiveEnabled || !state.AcceptingNewWork || h.starts.Load() != 1 {
				t.Fatal("enable applied the other level's saved disable", snapshot)
			}
			undo := stateChange(t, h, pendingTarget, "undo-other-level-disable", true, "drain")
			waitStatus(t, h, undo, "succeeded")
			if after := waitApplied(t, h.manager); after.RestartRequired || h.starts.Load() != 1 || h.stops.Load() != 0 {
				t.Fatal("undoing the pending switch rebuilt an already running worker", after)
			}
		})
	}
}

func TestSettingsApplyWithoutApplyingPendingDisable(t *testing.T) {
	for _, instance := range []string{"", "ncm"} {
		t.Run("instance="+instance, func(t *testing.T) {
			h := setup(t)
			initial := waitApplied(t, h.manager)
			disable := stateChange(t, h, instance, "disable-before-settings", false, "drain")
			waitStatus(t, h, disable, "succeeded")
			name := "Updated while restart pending"
			op, err := h.manager.Change(context.Background(), "admin", config.Change{
				ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: "settings-after-disable",
				PluginID: "musicfs.ncm", InstanceID: "ncm", Settings: &config.InstanceSettings{Name: &name},
			})
			if err != nil {
				t.Fatal(err)
			}
			waitStatus(t, h, op, "succeeded")
			snapshot := h.manager.Snapshot()
			state := snapshot.Items[0].Instances[0]
			if !snapshot.RestartRequired || snapshot.AppliedRevision != initial.AppliedRevision || state.RuntimeState != "ready" || !state.AcceptingNewWork || !state.EffectiveEnabled || state.Name != name {
				t.Fatal("settings applied a pending disable or lost the restart requirement", snapshot)
			}
			p, i, err := h.manager.InstanceConfig("ncm")
			if err != nil || !p.Enabled || !i.Enabled || i.Name != name || h.starts.Load() != 1 || h.stops.Load() != 1 {
				t.Fatal("settings were not applied to the current worker configuration", err)
			}
			active := h.manager.RuntimeConfig().Plugins[0]
			if !active.Enabled || !active.Instances[0].Enabled || active.Instances[0].Name != name {
				t.Fatal("runtime composition received saved disable switches", active)
			}
			saved := h.store.Current().Plugins[0]
			if instance == "" && saved.Enabled || instance == "ncm" && saved.Instances[0].Enabled {
				t.Fatal("runtime configuration projection changed the saved disable")
			}
			if !h.manager.Snapshot().RestartRequired {
				t.Fatal("runtime configuration projection cleared pending restart")
			}
		})
	}
}

func TestDisableDoesNotSupersedeAnInFlightSettingsApplication(t *testing.T) {
	h := setup(t)
	lease, err := h.manager.Gate("ncm").Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	name := "Settings still apply"
	op, err := h.manager.Change(context.Background(), "admin", config.Change{
		ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: "inflight-settings",
		PluginID: "musicfs.ncm", InstanceID: "ncm", Settings: &config.InstanceSettings{Name: &name},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h, op, "draining")
	disable := stateChange(t, h, "", "disable-during-settings", false, "force")
	waitStatus(t, h, disable, "succeeded")
	if lease.Context().Err() != nil {
		t.Fatal("disable canceled work admitted before the settings update")
	}
	lease.Close()
	waitStatus(t, h, op, "succeeded")
	state := h.manager.Snapshot().Items[0].Instances[0]
	if !h.manager.Snapshot().RestartRequired || state.RuntimeState != "ready" || state.Name != name || !state.EffectiveEnabled {
		t.Fatal("pending disable superseded the settings application", state)
	}
}

func TestDisablingAnAlreadyDisabledPluginNeedsNoRestart(t *testing.T) {
	h := setupDocument(t, strings.Replace(document, "    enabled: true", "    enabled: false", 1))
	disabled := change(t, h, "already-disabled", false, "force")
	waitStatus(t, h, disabled, "succeeded")
	if disabled.RestartRequired || waitApplied(t, h.manager).RestartRequired || h.starts.Load() != 0 || h.stops.Load() != 0 {
		t.Fatal("unchanged disabled state incorrectly required restart")
	}
}

package pluginproto_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestScanPlanBindsIndependentRootOptions(t *testing.T) {
	plan := pluginproto.ScanPlan{PlanVersion: 1, Mode: "full", PluginID: "local", PluginVersion: "1.0.0", SchemaRevision: "v1", ConfigRevision: "r1", Roots: []pluginproto.ScanRoot{{RootID: "flat", RootIdentity: "a"}, {RootID: "deep", RootIdentity: "b"}}, Options: pluginproto.ImportOptions{Recursive: true, ExcludePatterns: []string{}}}
	legacy, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	plan.RootOptions = map[string]pluginproto.ImportOptions{"flat": {ExcludePatterns: []string{}, SkipHidden: true}}
	digest, err := plan.Digest()
	if err != nil || digest == legacy {
		t.Fatal("root configuration is missing from the digest", err)
	}
	if plan.Contains("flat", "sub/song.wav") || plan.Contains("flat", ".hidden.wav") || !plan.Contains("deep", "sub/song.wav") {
		t.Fatal("root coverage applied another directory's options")
	}
	raw, _ := json.Marshal(plan)
	var saved pluginproto.ScanPlan
	if json.Unmarshal(raw, &saved) != nil {
		t.Fatal("plan round trip failed")
	}
	if restored, err := saved.Digest(); err != nil || restored != digest {
		t.Fatal("persisted root options changed digest", err)
	}
	canonical, err := plan.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	canonical.RootOptions["flat"] = plan.Options
	if plan.Contains("flat", "sub/song.wav") {
		t.Fatal("canonical plan aliases root option map")
	}
	plan.RootOptions["outside"] = plan.Options
	if _, err := plan.Canonical(); err == nil {
		t.Fatal("options for an unselected root accepted")
	}
}

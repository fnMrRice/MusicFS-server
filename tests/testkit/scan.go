package testkit

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

// ScanPlan is an explicit complete-range fixture for repository tests. Real
// worker tests obtain their root identity bindings from the authorized runtime.
func ScanPlan(rootIDs ...string) pluginproto.ScanPlan {
	p := pluginproto.ScanPlan{PlanVersion: 1, Mode: "full", PluginID: "musicfs.plugin.source.localfiles", PluginVersion: "0.1.0", SchemaRevision: "localfiles-import-v1", ConfigRevision: "test-config-v1", Options: pluginproto.ImportOptions{Recursive: true, MaxDepth: 64}}
	for _, id := range rootIDs {
		p.Roots = append(p.Roots, pluginproto.ScanRoot{RootID: id, RootIdentity: "test-" + id})
	}
	return p
}

func ScanPublication(j jobs.Job, items []library.Observation) library.ScanPublication {
	return library.ScanPublication{PlanDigest: j.PlanDigest, CompletedRoots: append([]pluginproto.ScanRoot(nil), j.Plan.Roots...), Observations: items}
}

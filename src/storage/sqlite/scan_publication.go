package sqlite

import (
	"reflect"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func validatePublication(j jobs.Job, p library.ScanPublication) (map[string]pluginproto.ScanScope, error) {
	roots := append([]pluginproto.ScanRoot(nil), p.CompletedRoots...)
	sort.Slice(roots, func(i, k int) bool { return roots[i].RootID < roots[k].RootID })
	if p.PlanDigest != j.PlanDigest || !reflect.DeepEqual(roots, j.Plan.Roots) {
		return nil, domain.Fail("CONFLICT", "Scan publication does not complete the requested plan")
	}
	scopes := make(map[string]pluginproto.ScanScope, len(roots))
	for _, root := range roots {
		scope, err := j.Plan.Scope(root.RootID)
		if err != nil {
			return nil, err
		}
		scopes[root.RootID] = scope
	}
	for _, o := range p.Observations {
		if !scopes[o.RootID].Contains(o.RootID, o.RelativePath) {
			return nil, domain.Fail("ACCESS_DENIED", "Observation is outside the scan scope")
		}
	}
	return scopes, nil
}

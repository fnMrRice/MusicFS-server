package sqlite

import (
	"reflect"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func validatePublication(j jobs.Job, p library.ScanPublication) (map[string]pluginproto.ScanCoverage, error) {
	roots := append([]pluginproto.ScanRoot(nil), p.CompletedRoots...)
	sort.Slice(roots, func(i, k int) bool { return roots[i].RootID < roots[k].RootID })
	if p.PlanDigest != j.PlanDigest || !reflect.DeepEqual(roots, j.Plan.Roots) {
		return nil, domain.Fail("CONFLICT", "Scan publication does not complete the requested plan")
	}
	scopes := make(map[string]pluginproto.ScanCoverage, len(roots))
	for _, root := range roots {
		scope, err := j.Plan.Scope(root.RootID)
		if err != nil {
			return nil, err
		}
		excluded, captured := p.ExcludedPaths[root.RootID]
		if (scope.SkipHidden || scope.RespectNoMedia) && !captured {
			return nil, domain.Fail("CONFLICT", "Filtered scan has no host coverage evidence")
		}
		coverage, err := pluginproto.NewScanCoverage(scope, excluded)
		if err != nil {
			return nil, err
		}
		scopes[root.RootID] = coverage
	}
	for rootID := range p.ExcludedPaths {
		if _, ok := scopes[rootID]; !ok {
			return nil, domain.Fail("CONFLICT", "Scan exclusions refer to an unselected root")
		}
	}
	for _, o := range p.Observations {
		if !scopes[o.RootID].Contains(o.RootID, o.RelativePath) {
			return nil, domain.Fail("ACCESS_DENIED", "Observation is outside the scan scope")
		}
	}
	return scopes, nil
}

package sqlite

import (
	"reflect"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

// 先对整个扫描按插件身份匹配，保留重命名与各硬链接的来源；再匹配原位替换。
// 只在同一根和完整路径各有一个未匹配的新旧来源时保留旧曲目。
func scanTrackBindings(p library.ScanPublication, old map[string]library.Track) (map[string]string, error) {
	type location struct{ root, path string }
	bindings := make(map[string]string, len(p.Observations))
	atPath := map[location]string{}
	for _, o := range p.Observations {
		if _, seen := bindings[o.ExternalID]; seen || o.ExternalID == "" || o.ObservedVersion == "" {
			return nil, domain.Fail("CONFLICT", "Scan contains an invalid or repeated track identity")
		}
		bindings[o.ExternalID] = ""
		if p.StablePaths {
			key := location{o.RootID, o.RelativePath}
			if _, exists := atPath[key]; exists {
				atPath[key] = ""
			} else {
				atPath[key] = o.ExternalID
			}
		}
	}
	replacements := map[string]string{}
	for id, t := range old {
		if _, present := bindings[t.ExternalID]; present {
			bindings[t.ExternalID] = id
			continue
		}
		next := atPath[location{t.RootID, t.Path}]
		if next == "" || t.Availability != "available" {
			continue
		}
		if _, exists := replacements[next]; exists {
			replacements[next] = ""
		} else {
			replacements[next] = id
		}
	}
	for external, id := range replacements {
		if bindings[external] == "" {
			bindings[external] = id
		}
	}
	return bindings, nil
}

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

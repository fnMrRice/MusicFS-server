package pluginproto

import (
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// HasHiddenPathSegment applies only to root-relative paths. Explicitly selected
// roots are not silently excluded because of a dot name or hidden attribute.
func HasHiddenPathSegment(relative string) bool {
	for _, segment := range strings.Split(relative, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

// ScanCoverage is trusted host evidence about a completed filesystem snapshot.
// Dynamic exclusions are not plugin-supplied options and never widen a scope.
type ScanCoverage struct {
	Scope    ScanScope
	excluded map[string]bool
}

func NewScanCoverage(scope ScanScope, excluded []string) (ScanCoverage, error) {
	c := ScanCoverage{Scope: scope, excluded: map[string]bool{}}
	if err := scope.Validate(); err != nil {
		return c, err
	}
	if len(excluded) > 50000 || len(excluded) > 0 && !scope.SkipHidden && !scope.RespectNoMedia {
		return c, fault.Fail("CONFLICT", "Invalid dynamic scan coverage")
	}
	bytes := 0
	for _, path := range excluded {
		bytes += len(path)
		if bytes > 8<<20 || path != "." && !ValidScanPath(path) || c.excluded[path] {
			return c, fault.Fail("CONFLICT", "Invalid dynamic scan exclusion")
		}
		c.excluded[path] = true
	}
	return c, nil
}

func (c ScanCoverage) Contains(root, relative string) bool {
	if !c.Scope.Contains(root, relative) || c.excluded["."] {
		return false
	}
	for {
		if c.excluded[relative] {
			return false
		}
		i := strings.LastIndexByte(relative, '/')
		if i < 0 {
			return true
		}
		relative = relative[:i]
	}
}

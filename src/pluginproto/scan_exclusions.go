package pluginproto

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (o ImportOptions) MarshalJSON() ([]byte, error) {
	type plain ImportOptions
	o.ExcludePatterns = append([]string{}, o.ExcludePatterns...)
	return json.Marshal(plain(o))
}

func (s ScanScope) MarshalJSON() ([]byte, error) {
	type plain ScanScope
	s.ExcludePatterns = append([]string{}, s.ExcludePatterns...)
	return json.Marshal(plain(s))
}

const MaxExcludePatterns = 32
const MaxExcludePatternBytes = 256

// CanonicalExclusions implements the small, portable scan glob dialect. Rules
// are root-relative and case-sensitive on every platform. A matching ancestor
// excludes its entire subtree; there are no include/negation rules.
func CanonicalExclusions(patterns []string) ([]string, error) {
	if len(patterns) > MaxExcludePatterns {
		return nil, domain.Fail("INVALID_ARGUMENT", "Too many scan exclusion patterns")
	}
	result := append([]string{}, patterns...)
	for _, pattern := range result {
		if len(pattern) == 0 || len(pattern) > MaxExcludePatternBytes || !utf8.ValidString(pattern) || strings.ContainsAny(pattern, "\\:[]{}!") || strings.IndexFunc(pattern, unicode.IsControl) >= 0 {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid scan exclusion pattern")
		}
		parts := strings.Split(pattern, "/")
		if len(parts) > MaxScanDepth+1 {
			return nil, domain.Fail("INVALID_ARGUMENT", "Scan exclusion pattern is too deep")
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || part != "**" && strings.Contains(part, "**") {
				return nil, domain.Fail("INVALID_ARGUMENT", "Invalid scan exclusion segment")
			}
		}
	}
	slices.Sort(result)
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, domain.Fail("INVALID_ARGUMENT", "Repeated scan exclusion pattern")
		}
	}
	return result, nil
}

// Excludes expects already validated rules and a canonical relative path. It
// uses bounded dynamic programming, never recursive wildcard backtracking.
func Excludes(patterns []string, relativePath string) bool {
	parts := strings.Split(relativePath, "/")
	for _, pattern := range patterns {
		matched := make([]bool, len(parts)+1)
		matched[0] = true
		for _, segment := range strings.Split(pattern, "/") {
			next := make([]bool, len(parts)+1)
			if segment == "**" {
				next[0] = matched[0]
				for i := 1; i <= len(parts); i++ {
					next[i] = matched[i] || next[i-1]
				}
			} else {
				for i, part := range parts {
					ok, _ := path.Match(segment, part)
					next[i+1] = matched[i] && ok
				}
			}
			matched = next
		}
		for _, yes := range matched {
			if yes {
				return true
			}
		}
	}
	return false
}

func (s ScanScope) Equal(other ScanScope) bool {
	return s.RootID == other.RootID && s.RootIdentity == other.RootIdentity && s.Recursive == other.Recursive && s.MaxDepth == other.MaxDepth && slices.Equal(s.ExcludePatterns, other.ExcludePatterns)
}

func (s ScanScope) ContainsDirectory(relativePath string) bool {
	if relativePath == "." {
		return s.Validate() == nil
	}
	return s.Validate() == nil && ValidScanPath(relativePath) && strings.Count(relativePath, "/")+1 <= s.MaxDepth && !Excludes(s.ExcludePatterns, relativePath)
}

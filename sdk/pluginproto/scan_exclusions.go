package pluginproto

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
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
		return nil, fault.Fail("INVALID_ARGUMENT", "Too many scan exclusion patterns")
	}
	result := append([]string{}, patterns...)
	for _, pattern := range result {
		if len(pattern) == 0 || len(pattern) > MaxExcludePatternBytes || !utf8.ValidString(pattern) || strings.ContainsAny(pattern, "\\:[]{}!") || strings.IndexFunc(pattern, unicode.IsControl) >= 0 {
			return nil, fault.Fail("INVALID_ARGUMENT", "Invalid scan exclusion pattern")
		}
		parts := strings.Split(pattern, "/")
		if len(parts) > MaxScanDepth+1 {
			return nil, fault.Fail("INVALID_ARGUMENT", "Scan exclusion pattern is too deep")
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || part != "**" && strings.Contains(part, "**") {
				return nil, fault.Fail("INVALID_ARGUMENT", "Invalid scan exclusion segment")
			}
		}
	}
	slices.Sort(result)
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, fault.Fail("INVALID_ARGUMENT", "Repeated scan exclusion pattern")
		}
	}
	return result, nil
}

// Excludes expects already validated rules and a canonical relative path. It
// uses bounded dynamic programming, never recursive wildcard backtracking.
func Excludes(patterns []string, relativePath string) bool {
	if len(patterns) == 0 {
		return false
	}
	parts := strings.Split(relativePath, "/")
	matched := make([]bool, len(parts)+1)
	for _, pattern := range patterns {
		clear(matched)
		matched[0] = true
		for segment := range strings.SplitSeq(pattern, "/") {
			if segment == "**" {
				// ** 可以不消耗路径段，也可以继续向右匹配任意多个段。
				for i := 1; i <= len(parts); i++ {
					matched[i] = matched[i] || matched[i-1]
				}
			} else {
				// 普通段只消耗一段；从后往前写，保留上一轮的前缀状态。
				for i := len(parts); i > 0; i-- {
					matched[i] = matched[i-1]
					if matched[i] {
						matched[i], _ = path.Match(segment, parts[i-1])
					}
				}
				matched[0] = false
			}
		}
		if slices.Contains(matched, true) {
			return true
		}
	}
	return false
}

func (s ScanScope) Equal(other ScanScope) bool {
	return s.RootID == other.RootID && s.RootIdentity == other.RootIdentity && s.Recursive == other.Recursive && s.MaxDepth == other.MaxDepth && s.SkipHidden == other.SkipHidden && s.RespectNoMedia == other.RespectNoMedia && slices.Equal(s.ExcludePatterns, other.ExcludePatterns)
}

func (s ScanScope) ContainsDirectory(relativePath string) bool {
	if relativePath == "." {
		return s.Validate() == nil
	}
	return s.Validate() == nil && ValidScanPath(relativePath) && s.containsDepth(strings.Count(relativePath, "/")+1) && !Excludes(s.ExcludePatterns, relativePath) && (!s.SkipHidden || !HasHiddenPathSegment(relativePath))
}

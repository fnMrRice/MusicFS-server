package pluginproto

import (
	"regexp"
	"strings"
)

var pluginVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?(\+([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$`)

// ValidPluginVersion accepts a bounded semantic version. Plugin release
// versions evolve independently; wire compatibility uses ProtocolVersion and
// OnlineRevision, not an exact plugin release number.
func ValidPluginVersion(version string) bool {
	if len(version) > 128 || !pluginVersion.MatchString(version) {
		return false
	}
	base, _, _ := strings.Cut(version, "+")
	_, prerelease, hasPrerelease := strings.Cut(base, "-")
	if !hasPrerelease {
		return true
	}
	for _, identifier := range strings.Split(prerelease, ".") {
		numeric := true
		for _, c := range identifier {
			numeric = numeric && c >= '0' && c <= '9'
		}
		if numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

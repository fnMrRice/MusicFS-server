package pluginversions

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var releaseTitle = regexp.MustCompile(`^chore\(release\): publish v0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type increment int

const (
	unchanged increment = iota
	patch
	minor
)

var commitHeader = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9-]*)(?:\([^()\r\n]+\))?(!)?:[ \t]+\S`)
var breakingFooter = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGES?:[ \t]*\S`)

func classifyCommit(message string) increment {
	subject, body, _ := strings.Cut(message, "\n")
	header := commitHeader.FindStringSubmatch(subject)
	if breakingFooter.MatchString(body) {
		return minor
	}
	if header == nil {
		if strings.HasPrefix(subject, `Revert "`) {
			return patch
		}
		return unchanged
	}
	if header[2] == "!" {
		return minor
	}
	switch strings.ToLower(header[1]) {
	case "feat":
		return minor
	case "fix", "perf", "refactor", "revert":
		return patch
	default:
		return unchanged
	}
}

// 次版本从新的修订周期开始，后续修订在该次版本内递增。
func (b increment) apply(version string) (string, error) {
	parts := strings.Split(version, ".")
	index := 1
	if b == patch {
		index = 2
	}
	value, err := strconv.ParseUint(parts[index], 10, 64)
	if err != nil || value == math.MaxUint64 {
		return "", fmt.Errorf("invalid or overflowing counter in %q", version)
	}
	parts[index] = strconv.FormatUint(value+1, 10)
	if b == minor {
		parts[2] = "0"
	}
	return strings.Join(parts, "."), nil
}

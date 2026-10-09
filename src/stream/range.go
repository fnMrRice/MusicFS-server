package stream

import (
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// ValidateRangeHeader separates malformed syntax from unsatisfied byte ranges.
// Content length and multipart responses remain the standard library's job.
func ValidateRangeHeader(value string) error {
	if value == "" {
		return nil
	}
	invalid := func() error { return domain.Fail("INVALID_ARGUMENT", "Invalid or excessive byte ranges") }
	if !strings.HasPrefix(value, "bytes=") {
		return invalid()
	}
	ranges := strings.Split(strings.TrimPrefix(value, "bytes="), ",")
	if len(ranges) > 16 {
		return invalid()
	}
	for _, raw := range ranges {
		start, end, ok := strings.Cut(strings.TrimSpace(raw), "-")
		if !ok || start == "" && end == "" {
			return invalid()
		}
		var first, last int64
		for i, value := range []string{start, end} {
			if value == "" {
				continue
			}
			for _, char := range value {
				if char < '0' || char > '9' {
					return invalid()
				}
			}
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return invalid()
			}
			if i == 0 {
				first = n
			} else {
				last = n
			}
		}
		if start != "" && end != "" && last < first {
			return invalid()
		}
	}
	return nil
}

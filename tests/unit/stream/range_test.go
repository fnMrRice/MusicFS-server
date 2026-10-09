package stream_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/stream"
)

func TestRangeSyntaxIsSeparateFromSatisfiability(t *testing.T) {
	for _, value := range []string{"", "bytes=0-1", "bytes=0-", "bytes=-32", "bytes=0-1, 8-20", "bytes=999999999-", "bytes=-0"} {
		if err := stream.ValidateRangeHeader(value); err != nil {
			t.Errorf("valid byte-range syntax %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"none", "units=0-1", "bytes=", "bytes=-", "bytes=3-2", "bytes=0-1,", "bytes=+1-2", "bytes=--1", "bytes=0-1-2", "bytes=1 -2", "bytes=9223372036854775808-", "bytes=" + strings.Repeat("0-1,", 16) + "0-1"} {
		if err := stream.ValidateRangeHeader(value); err == nil {
			t.Errorf("invalid byte-range syntax %q accepted", value)
		}
	}
}

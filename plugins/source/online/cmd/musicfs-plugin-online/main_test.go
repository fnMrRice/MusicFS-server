package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

type outputBuffer struct{ bytes.Buffer }

func (*outputBuffer) Close() error { return nil }

func TestInvalidArgumentsExitBeforeOpeningProtocolChannel(t *testing.T) {
	for _, args := range [][]string{
		nil, {"--plugin", "unknown"}, {"--plugin", "netease", "extra"},
		{"--plugin", "netease", "--plugin", "qqmusic"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout outputBuffer
			var stderr bytes.Buffer
			if code := run(args, io.NopCloser(strings.NewReader("")), &stdout, &stderr); code != 2 {
				t.Fatalf("expected argument exit status 2, got %d", code)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatal("argument errors must only write a message to stderr")
			}
			for _, r := range stderr.String() {
				if r > 127 {
					t.Fatal("argument error must use English output")
				}
			}
		})
	}
}

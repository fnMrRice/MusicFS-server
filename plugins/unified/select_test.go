package unified_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/unified"
)

func TestSelectAcceptsOneKnownRole(t *testing.T) {
	for _, role := range []string{
		"local", "audio-ffmpeg", "ncm", "scraper-local", "netease",
		"qqmusic", "applemusic", "storage-sqlite", "dsp-gain",
	} {
		for _, args := range [][]string{{"--plugin", role}, {"--plugin=" + role}} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				worker, err := unified.Select(args)
				if err != nil || worker == nil {
					t.Fatal("known plugin selection failed", err)
				}
			})
		}
	}
}

func TestSelectRejectsMissingUnknownAndAmbiguousArguments(t *testing.T) {
	for _, args := range [][]string{
		nil, {"local"}, {"--plugin"}, {"--plugin="}, {"--plugin", ""},
		{"--plugin", "unknown"}, {"--plugin", "SQLITE"}, {"--unknown", "local"},
		{"--plugin", "local", "extra"}, {"--plugin=netease", "extra"},
		{"--plugin", "local", "--plugin", "dsp-gain"},
		{"--plugin=local", "--plugin=local"}, {"--", "--plugin", "local"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if worker, err := unified.Select(args); err == nil || worker != nil {
				t.Fatal("invalid arguments selected a worker")
			}
		})
	}
}

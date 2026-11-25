package pluginproto_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestScanRulesMatchWholeSegmentsAndAncestors(t *testing.T) {
	rules := []string{"disc/**/bonus/**/draft?.flac", "outtakes/**", "**/缓存", "archive"}
	for name, excluded := range map[string]bool{
		"disc/bonus/draft1.flac": true, "disc/2/bonus/live/draft甲.flac": true,
		"disc/2/bonus/draft12.flac": false, "disc/demo/draft1.flac": false,
		"archive/disc/track.flac": true, "live/archive/track.flac": false,
		"outtakes": true, "album/缓存/track.flac": true, "album/track.flac": false,
	} {
		if got := pluginproto.Excludes(rules, name); got != excluded {
			t.Errorf("Excludes(%q) = %v, want %v", name, got, excluded)
		}
	}
}

func BenchmarkScanExclusions(b *testing.B) {
	const file = "music/artist/album/disc-1/track-03.flac"
	for _, tc := range []struct {
		name  string
		rules []string
	}{
		{"Unfiltered", nil},
		{"Filtered", []string{"archive/**", "**/cache", "**/*.tmp", "music/**/demo/*"}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if pluginproto.Excludes(tc.rules, file) {
					b.Fatal("unexpected exclusion")
				}
			}
		})
	}
}

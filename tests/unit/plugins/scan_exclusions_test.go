package plugins_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanExclusionsMatchPortablePathsAndAncestors(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		excluded      bool
	}{
		{"*.mp3", "track.mp3", true}, {"*.mp3", "disc/track.mp3", false},
		{"**/*.mp3", "track.mp3", true}, {"**/*.mp3", "disc/sub/track.mp3", true},
		{"**/cache", "cache/track.wav", true}, {"**/cache", "disc/cache/sub/track.wav", true},
		{"cache", "disc/cache/track.wav", false}, {"cache", "cache2/track.wav", false},
		{"disc/?音.wav", "disc/中音.wav", true}, {"disc/?音.wav", "disc/中文音.wav", false},
		{"**/.*", "disc/.private/track.wav", true}, {"**/.*", "disc/track.wav", false},
		{"Track.*", "track.wav", false}, {"disc/**/x.wav", "disc/x.wav", true},
		{"disc/**/x.wav", "disc/a/b/x.wav", true}, {"**", "a.wav", true},
	} {
		rules, err := pluginproto.CanonicalExclusions([]string{tc.pattern})
		if err != nil || pluginproto.Excludes(rules, tc.path) != tc.excluded {
			t.Errorf("pattern %q path %q: %v", tc.pattern, tc.path, err)
		}
	}
	for _, value := range []string{"", "/private", "../private", "a/../b", "a/./b", "a//b", "a/", `a\b`, "C:track", "a/**x", "[ab]", "!private", "{a,b}", "a\x00b", "a\nb", strings.Repeat("a", 257), string([]byte{0xff}), strings.Repeat("x/", 65) + "x"} {
		if _, err := pluginproto.CanonicalExclusions([]string{value}); err == nil {
			t.Errorf("invalid rule accepted: %q", value)
		}
	}
	if _, err := pluginproto.CanonicalExclusions([]string{"x", "x"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := pluginproto.CanonicalExclusions(make([]string, 33)); err == nil {
		t.Fatal("too many rules accepted")
	}
}

func TestScanExclusionsBindPlanAndCloneOverrides(t *testing.T) {
	plan := testkit.ScanPlan("root")
	plan.Options.ExcludePatterns = []string{"z", "a"}
	first, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	plan.Options.ExcludePatterns = []string{"a", "z"}
	second, _ := plan.Digest()
	if first != second {
		t.Fatal("rule order changed set semantics")
	}
	plan.Options.ExcludePatterns = []string{"a"}
	third, _ := plan.Digest()
	if third == first {
		t.Fatal("exclusions not bound to plan digest")
	}
	scope, err := plan.Scope("root")
	if err != nil {
		t.Fatal(err)
	}
	if scope.Contains("root", "a/track.wav") || scope.ContainsDirectory("a") || !scope.ContainsDirectory(".") {
		t.Fatal("directory exclusion escaped scope")
	}
	var patch pluginproto.ImportOverrides
	if err = json.Unmarshal([]byte(`{"excludePatterns":["z","a"]}`), &patch); err != nil {
		t.Fatal(err)
	}
	clone := patch.Clone()
	(*clone.ExcludePatterns)[0] = "changed"
	if (*patch.ExcludePatterns)[0] != "z" {
		t.Fatal("override clone aliases source")
	}
	base := pluginconfig.LocalFiles().ImportDefaults
	merged, err := pluginproto.MergeImportOptions(base, patch)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(merged.ExcludePatterns, []string{"a", "z"}) {
		t.Fatal("rules not canonical")
	}
	empty := []string{}
	cleared, err := pluginproto.MergeImportOptions(merged, pluginproto.ImportOverrides{ExcludePatterns: &empty})
	if err != nil || cleared.ExcludePatterns == nil || len(cleared.ExcludePatterns) != 0 {
		t.Fatal("explicit empty did not clear inherited rules")
	}
	for _, raw := range []string{`{"excludePatterns":null}`, `{"excludePatterns":[1]}`, `{"excludePatterns":"x"}`} {
		if json.Unmarshal([]byte(raw), &patch) == nil {
			t.Fatal("invalid rules shape accepted")
		}
	}
}

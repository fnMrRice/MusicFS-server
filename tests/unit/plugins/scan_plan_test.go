package plugins_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanPlanCanonicalDigestBindsEveryInput(t *testing.T) {
	p := testkit.ScanPlan("z", "a")
	before := append([]pluginproto.ScanRoot(nil), p.Roots...)
	first, err := p.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, p.Roots) {
		t.Fatal("canonicalization changed caller roots")
	}
	reordered := testkit.ScanPlan("a", "z")
	second, err := reordered.Digest()
	if err != nil || first != second || len(first) != 64 {
		t.Fatal("canonical root order changed digest", err)
	}
	changes := []func(*pluginproto.ScanPlan){
		func(p *pluginproto.ScanPlan) { p.Mode = "incremental" },
		func(p *pluginproto.ScanPlan) { p.PluginID = "other" },
		func(p *pluginproto.ScanPlan) { p.PluginVersion = "0.2.1" },
		func(p *pluginproto.ScanPlan) { p.SchemaRevision = "schema-v2" },
		func(p *pluginproto.ScanPlan) { p.ConfigRevision = "config-v2" },
		func(p *pluginproto.ScanPlan) { p.Roots[0].RootIdentity = "replacement" },
		func(p *pluginproto.ScanPlan) { p.Roots = p.Roots[:1] },
		func(p *pluginproto.ScanPlan) { p.Options.MaxDepth = 1 },
		func(p *pluginproto.ScanPlan) { p.Options.ImportLyrics = true },
		func(p *pluginproto.ScanPlan) { p.Options = pluginproto.ImportOptions{} },
	}
	for i, change := range changes {
		candidate := testkit.ScanPlan("z", "a")
		change(&candidate)
		digest, err := candidate.Digest()
		if err != nil || digest == first {
			t.Fatalf("unbound plan field %d: %v", i, err)
		}
	}
	for _, p := range []pluginproto.ScanPlan{testkit.ScanPlan(), testkit.ScanPlan("a", "a"), testkit.ScanPlan("../a"), testkit.ScanPlan(strings.Repeat("x", 513))} {
		if _, err := p.Digest(); err == nil {
			t.Fatal("invalid plan accepted")
		}
	}
	lyrics := testkit.ScanPlan("a")
	lyrics.Options.ImportLyrics = true
	if _, err := lyrics.Digest(); err != nil {
		t.Fatal("lyrics import plan rejected", err)
	}
}

func TestScanScopeUsesCanonicalLocalDepth(t *testing.T) {
	flat := pluginproto.ScanScope{RootID: "root", RootIdentity: "identity"}
	deep := flat
	deep.Recursive = true
	deep.MaxDepth = 1
	for _, name := range []string{"a.wav", "中文 文件.mp3"} {
		if !flat.Contains("root", name) || !deep.Contains("root", name) {
			t.Fatal("root file excluded")
		}
	}
	if flat.Contains("root", "sub/a.wav") || !deep.Contains("root", "sub/a.wav") || deep.Contains("root", "sub/deeper/a.wav") {
		t.Fatal("depth boundary is incorrect")
	}
	for _, path := range []string{"", ".", "../a", "a/../b", "/a", "a//b", "a\\b", "C:/a", "a/./b", "a\x00b", "a/"} {
		if deep.Contains("root", path) {
			t.Fatalf("invalid path accepted: %q", path)
		}
	}
	if deep.Contains("other", "a") {
		t.Fatal("another root accepted")
	}
	for _, scope := range []pluginproto.ScanScope{{RootID: "root", RootIdentity: "id", Recursive: true}, {RootID: "root", RootIdentity: "id", MaxDepth: 1}, {RootID: "root", RootIdentity: "id", Recursive: true, MaxDepth: 65}, {RootID: "root"}} {
		if scope.Validate() == nil {
			t.Fatal("invalid scope accepted")
		}
	}
}

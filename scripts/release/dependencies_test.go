package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestReleaseReplacementPolicy(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"sdk", "plugins/source/online", "unreviewed"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	b := builder{root: root}
	for _, source := range sourceModules[1:] {
		mod := module{Path: source.Path, Replace: &module{Dir: filepath.Join(root, source.Directory)}}
		got, err := b.reviewedReplacement(mod)
		if err != nil || got != source.Directory {
			t.Fatalf("approved source rejected: %s: %v", got, err)
		}
		mod.Replace.Dir = filepath.Join(root, "unreviewed")
		if _, err := b.reviewedReplacement(mod); err == nil {
			t.Fatal("replacement outside the approved source was accepted")
		}
	}
	if _, err := b.reviewedReplacement(module{Path: "example.com/third-party", Replace: &module{Dir: filepath.Join(root, "sdk")}}); err == nil {
		t.Fatal("third-party replacement was treated as first-party SDK")
	}
}

func TestDependenciesIncludeAllModuleGraphsAndLicenses(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	b := builder{root: root, out: t.TempDir(), env: targetEnvironment(runtime.GOOS, runtime.GOARCH)}
	deps, err := b.dependencies()
	if err != nil {
		t.Fatal(err)
	}
	foundSDK, foundOnline, foundThirdParty := false, false, false
	for _, dep := range deps {
		switch dep.Path {
		case "github.com/fnMrRice/MusicFS-server/sdk":
			foundSDK = true
			if !dep.FirstParty || !dep.Compiled || dep.Source != "sdk" ||
				!slices.Contains(dep.Modules, "github.com/fnMrRice/MusicFS-server/plugins/source/online") {
				t.Fatalf("SDK dependency was not tracked: %+v", dep)
			}
		case "github.com/fnMrRice/MusicFS-server/plugins/source/online":
			foundOnline = true
			if !dep.FirstParty || !dep.Compiled || dep.Source != "plugins/source/online" {
				t.Fatalf("online module was not tracked: %+v", dep)
			}
		default:
			if !dep.Compiled {
				continue
			}
			foundThirdParty = true
			if len(dep.Licenses) == 0 {
				t.Fatalf("compiled dependency has no licenses: %+v", dep)
			}
			for _, license := range dep.Licenses {
				if _, err := os.Stat(filepath.Join(b.out, license)); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if !foundSDK || !foundOnline || !foundThirdParty {
		t.Fatalf("incomplete dependency accounting: sdk=%v online=%v external=%v", foundSDK, foundOnline, foundThirdParty)
	}
}

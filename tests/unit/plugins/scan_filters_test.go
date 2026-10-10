package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func TestHiddenNoMediaDefaultsAndLegacyPlanDigest(t *testing.T) {
	c := pluginconfig.LocalFiles()
	if !c.ImportDefaults.SkipHidden || !c.ImportDefaults.RespectNoMedia {
		t.Fatal("new defaults must exclude hidden and marked media")
	}
	var patch pluginproto.ImportOverrides
	if err := json.Unmarshal([]byte(`{"skipHidden":false,"respectNoMedia":false}`), &patch); err != nil {
		t.Fatal(err)
	}
	options, err := pluginproto.MergeImportOptions(c.ImportDefaults, patch)
	if err != nil || options.SkipHidden || options.RespectNoMedia {
		t.Fatal("explicit filter overrides lost", err)
	}
	clone := patch.Clone()
	*clone.SkipHidden, *clone.RespectNoMedia = true, true
	if *patch.SkipHidden || *patch.RespectNoMedia {
		t.Fatal("filter override clone mutated original")
	}
	c.ImportDefaults = options
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(body, &value)
	defaults := value["importDefaults"].(map[string]any)
	if defaults["skipHidden"] != false || defaults["respectNoMedia"] != false {
		t.Fatal("configuration omitted explicit false filter settings")
	}
	const legacy = `{"planVersion":1,"mode":"full","pluginId":"musicfs.localfiles","pluginVersion":"0.3.0","schemaRevision":"local-import-v3","configRevision":"old","roots":[{"rootId":"root","rootIdentity":"identity"}],"options":{"recursive":true,"maxDepth":64,"importLyrics":false,"excludePatterns":[]}}`
	var plan pluginproto.ScanPlan
	if err := json.Unmarshal([]byte(legacy), &plan); err != nil {
		t.Fatal(err)
	}
	digest, err := plan.Digest()
	expected := sha256.Sum256(append([]byte("musicfs-scan-plan-v1\x00"), []byte(legacy)...))
	if err != nil || digest != hex.EncodeToString(expected[:]) || plan.Options.SkipHidden || plan.Options.RespectNoMedia {
		t.Fatal("legacy plan digest or filter meaning changed", err)
	}
	plan.Options.SkipHidden = true
	changed, _ := plan.Digest()
	if changed == digest {
		t.Fatal("hidden option missing from plan binding")
	}
}

func filteredNames(t *testing.T, access *sourceaccess.Access, scope pluginproto.ScanScope) ([]string, []string) {
	t.Helper()
	entries, _, issues, excluded, err := access.EntriesWithExclusions(context.Background(), scope)
	if err != nil || len(issues) != 0 {
		t.Fatal("filtered scan failed", issues, err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.RelativePath)
	}
	sort.Strings(names)
	return names, excluded
}

func TestScanHiddenPathsNoMediaAndExplicitRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".selected-root")
	for _, name := range []string{"normal.wav", ".hidden.wav", ".private/hidden.wav", "album/song.wav", "album/deep/song.wav"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("audio"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// A marker directory counts as present without attempting to read contents.
	if err := os.Mkdir(filepath.Join(root, "album", ".nomedia"), 0700); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, SkipHidden: true, RespectNoMedia: true}
	names, excluded := filteredNames(t, access, scope)
	if !reflect.DeepEqual(names, []string{"normal.wav"}) {
		t.Fatal("default filters escaped or explicitly selected root was hidden", names)
	}
	coverage, err := pluginproto.NewScanCoverage(scope, excluded)
	if err != nil || coverage.Contains("root", "album/deep/song.wav") || coverage.Contains("root", ".hidden.wav") || !coverage.Contains("root", "normal.wav") {
		t.Fatal("dynamic coverage does not preserve skipped branches", err)
	}
	dirs, err := access.WatchDirectories(context.Background(), scope, 256)
	watch := []string{}
	for _, dir := range dirs {
		watch = append(watch, dir.RelativePath)
		dir.File.Close()
	}
	sort.Strings(watch)
	if err != nil || !reflect.DeepEqual(watch, []string{".", "album"}) {
		t.Fatal("watch must retain marker directory but skip descendants", watch, err)
	}
	scope.SkipHidden = false
	names, _ = filteredNames(t, access, scope)
	if len(names) != 3 {
		t.Fatal("hidden override did not reveal dot paths", names)
	}
	scope.RespectNoMedia = false
	names, _ = filteredNames(t, access, scope)
	if len(names) != 5 {
		t.Fatal("nomedia override did not reveal marked branch", names)
	}
	scope.SkipHidden, scope.RespectNoMedia = true, true
	if err := os.WriteFile(filepath.Join(root, ".nomedia"), []byte("unread marker contents"), 0000); err != nil {
		t.Fatal(err)
	}
	names, excluded = filteredNames(t, access, scope)
	if len(names) != 0 || !reflect.DeepEqual(excluded, []string{"."}) {
		t.Fatal("root marker did not exclude the selected root", names, excluded)
	}
	if err := os.Chmod(filepath.Join(root, ".nomedia"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".nomedia")); err != nil {
		t.Fatal(err)
	}
	names, _ = filteredNames(t, access, scope)
	if len(names) != 1 {
		t.Fatal("marker removal did not restore scan coverage")
	}
}

func TestHiddenSymlinksAreSkippedBeforeVisibleLinkWarnings(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.wav"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "normal.wav"), []byte("normal"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".hidden-link")); err != nil {
		t.Skip("symbolic links unavailable: " + err.Error())
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.ScanScope{RootID: "root", RootIdentity: access.Roots()[0].ExpectedIdentity, Recursive: true, SkipHidden: true}
	entries, _, issues, excluded, err := access.EntriesWithExclusions(context.Background(), scope)
	if err != nil || len(issues) != 0 || len(entries) != 1 || len(excluded) != 1 {
		t.Fatal("hidden link blocked scan or escaped root", issues, err)
	}
	if err := os.Rename(filepath.Join(root, ".hidden-link"), filepath.Join(root, "visible-link")); err != nil {
		t.Fatal(err)
	}
	entries, _, issues, _, err = access.EntriesWithExclusions(context.Background(), scope)
	if err != nil || len(issues) != 1 || len(entries) != 1 {
		t.Fatal("visible link changed the existing incomplete-scan boundary", issues, err)
	}
}

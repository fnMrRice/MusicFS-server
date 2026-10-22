package logging_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

func TestArchiveStartupRecoveryAndInactivePluginRetention(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(plugins, 0700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Add(-time.Hour)
	source := archiveFixture(t, dir, "server", stamp, "uncompressed source\n")
	const inactiveID = "musicfs.plugin.removed"
	inactive := filepath.Join(plugins, inactiveID+".log")
	expired := filepath.Join(plugins, "musicfs.plugin.expired.log")
	current := filepath.Join(plugins, "musicfs.plugin.current.log")
	unknown := filepath.Join(plugins, "not a plugin.log")
	for _, path := range []string{inactive, expired, current, unknown} {
		if err := os.WriteFile(path, []byte("inactive plugin record\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inactiveStamp := time.Now().UTC().Add(-5 * 24 * time.Hour)
	for path, modified := range map[string]time.Time{inactive: inactiveStamp, expired: time.Now().Add(-31 * 24 * time.Hour), unknown: time.Now().Add(-40 * 24 * time.Hour)} {
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	newManager(t, dir, io.Discard)
	assertMissing(t, source)
	assertMissing(t, inactive)
	assertMissing(t, expired)
	if contents(t, current) != "inactive plugin record\n" || contents(t, unknown) != "inactive plugin record\n" {
		t.Fatal("current or unknown file changed")
	}
	if entries := readBundle(t, bundlePath(dir, stamp)); entries[filepath.Base(source)] != "uncompressed source\n" {
		t.Fatal("startup recovery lost its source")
	}
	entries := readBundle(t, bundlePath(dir, inactiveStamp))
	if len(entries) != 1 || !hasBundleContent(entries, "inactive plugin record\n") {
		t.Fatal("inactive plugin log was not archived")
	}
	for name := range entries {
		if !strings.HasPrefix(name, "plugins/"+inactiveID+"-") {
			t.Fatalf("plugin relative path was not preserved: %s", name)
		}
	}
}

func TestArchiveRepeatedRotationAppendsToSameDayBundle(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, io.Discard)
	stamp := time.Now().UTC()
	m.Logger().Info("first server record")
	m.Logger().Info("first plugin record", "pluginId", pluginID)
	if err := m.Rotate(); err != nil {
		t.Fatal(err)
	}
	first := waitBundle(t, bundlePath(dir, stamp), 2)
	m.Logger().Info("second server record")
	m.Logger().Info("second plugin record", "pluginId", pluginID)
	if err := m.Rotate(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	paths := bundlePaths(t, dir)
	if len(paths) != 1 {
		t.Fatalf("same-day rotation created separate archives: %v", paths)
	}
	entries := readBundle(t, paths[0])
	if len(entries) != 4 {
		t.Fatalf("repeated rotation lost members: %v", entries)
	}
	for name, data := range first {
		if entries[name] != data {
			t.Fatalf("previous member was changed: %s", name)
		}
	}
	var combined string
	for _, data := range entries {
		combined += data
	}
	for _, message := range []string{"first server record", "first plugin record", "second server record", "second plugin record"} {
		if strings.Count(combined, message) != 1 {
			t.Fatalf("record was lost or duplicated: %s", message)
		}
	}
}

func TestArchiveStartupGroupsSourcesByUTCDay(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(plugins, 0700); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var sources []string
	for _, stamp := range []time.Time{today.Add(10 * time.Minute), today.Add(-24*time.Hour + 10*time.Minute)} {
		sources = append(sources, archiveFixture(t, dir, "server", stamp, "server "+stamp.Format(time.DateOnly)))
		sources = append(sources, archiveFixture(t, plugins, pluginID, stamp, "plugin "+stamp.Format(time.DateOnly)))
	}
	newManager(t, dir, io.Discard)
	if paths := bundlePaths(t, dir); len(paths) != 2 {
		t.Fatalf("different days were combined: %v", paths)
	}
	for _, source := range sources {
		assertMissing(t, source)
	}
	for _, stamp := range []time.Time{today, today.Add(-24 * time.Hour)} {
		entries := readBundle(t, bundlePath(dir, stamp))
		if len(entries) != 2 || !hasBundleContent(entries, "server "+stamp.Format(time.DateOnly)) || !hasBundleContent(entries, "plugin "+stamp.Format(time.DateOnly)) {
			t.Fatalf("daily archive contents are wrong: %v", entries)
		}
	}
}

func TestArchiveExistingBundleDeduplicatesMatchingSource(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Now().UTC().Add(-time.Hour)
	source := archiveFixture(t, dir, "server", stamp, "matching record\n")
	additional := archiveFixture(t, dir, "server", stamp.Add(time.Millisecond), "additional record\n")
	priorName := "plugins/" + pluginID + "-" + stamp.Format("2006-01-02T15-04-05.000") + ".log"
	target := bundlePath(dir, stamp)
	bundleFixture(t, target, map[string]string{filepath.Base(source): "matching record\n", priorName: "prior plugin record\n"})
	newManager(t, dir, io.Discard)
	assertMissing(t, source)
	assertMissing(t, additional)
	entries := readBundle(t, target)
	if len(entries) != 3 || entries[filepath.Base(source)] != "matching record\n" || entries[filepath.Base(additional)] != "additional record\n" || entries[priorName] != "prior plugin record\n" {
		t.Fatalf("matching source recovery lost or duplicated a member: %v", entries)
	}
}

func TestArchiveFailedMergePreservesSourceAndPublishedBundle(t *testing.T) {
	for _, scenario := range []string{"different", "longer", "corrupt", "truncated-trailer", "destination-directory"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			stamp := time.Now().UTC().Add(-time.Hour)
			source := archiveFixture(t, dir, "server", stamp, "source record\n")
			target := bundlePath(dir, stamp)
			var before []byte
			if scenario == "destination-directory" {
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				data := "other record\n"
				if scenario == "longer" {
					data = strings.Repeat("important distinct archive\n", 5)
				}
				if scenario == "truncated-trailer" {
					data = "source record\n"
				}
				bundleFixture(t, target, map[string]string{filepath.Base(source): data})
				if scenario == "corrupt" {
					if err := os.WriteFile(target, []byte("incomplete archive"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "truncated-trailer" {
					data := []byte(contents(t, target))
					if err := os.WriteFile(target, data[:len(data)-4], 0600); err != nil {
						t.Fatal(err)
					}
				}
				before = []byte(contents(t, target))
			}
			manager, err := logging.New(logging.Options{Directory: dir, Console: io.Discard})
			if err == nil {
				manager.Close()
				t.Fatal("invalid archive destination or conflicting member was accepted")
			}
			if contents(t, source) != "source record\n" {
				t.Fatal("failed merge changed the source")
			}
			if scenario != "destination-directory" && !bytes.Equal([]byte(contents(t, target)), before) {
				t.Fatal("failed merge overwrote the published archive")
			}
		})
	}
}

func TestArchiveDailyRetentionIncludesCutoffDay(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	cutoff := today.AddDate(0, 0, -logging.RetentionDays)
	expired := bundlePath(dir, cutoff.AddDate(0, 0, -1))
	retained := bundlePath(dir, cutoff)
	unrelated := filepath.Join(dir, "other-2000-01-01.tar.gz")
	for _, path := range []string{expired, retained, unrelated} {
		bundleFixture(t, path, map[string]string{"server-" + cutoff.Format("2006-01-02T15-04-05.000") + ".log": "record\n"})
	}
	newManager(t, dir, io.Discard)
	assertMissing(t, expired)
	if entries := readBundle(t, retained); !hasBundleContent(entries, "record\n") {
		t.Fatal("cutoff-day archive was removed")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated archive was removed")
	}
}

func TestArchiveCloseWaitsForCompression(t *testing.T) {
	dir := t.TempDir()
	manager := newManager(t, dir, io.Discard)
	manager.Logger().Info("archive before close")
	if err := manager.Rotate(); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	paths := bundlePaths(t, dir)
	if len(paths) != 1 {
		t.Fatalf("close did not complete compression: %v", paths)
	}
	entries := readBundle(t, paths[0])
	if len(entries) != 1 {
		t.Fatalf("closed archive has unexpected members: %v", entries)
	}
	for name, data := range entries {
		if !strings.Contains(data, "archive before close") {
			t.Fatal("closed archive is incomplete")
		}
		assertMissing(t, filepath.Join(dir, filepath.FromSlash(name)))
	}
}

func TestArchiveCleanupIgnoresSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte("outside marker"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "logs-2000-01-01.tar.gz")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	newManager(t, dir, io.Discard)
	if _, err := os.Lstat(link); err != nil || contents(t, outside) != "outside marker" {
		t.Fatal("cleanup touched a symbolic link or its target")
	}
}

package logging_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveTemporaryBundleRequiresCompleteCopiesOfEveryMember(t *testing.T) {
	for _, scenario := range []string{"published", "sources", "partial-published", "different-published", "sole-copy", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "plugins"), 0700); err != nil {
				t.Fatal(err)
			}
			stamp := time.Now().UTC().Add(-time.Hour)
			suffix := "-" + stamp.Format("2006-01-02T15-04-05.000") + ".log"
			server := "server" + suffix
			plugin := "plugins/" + pluginID + suffix
			entries := map[string]string{server: "server record\n", plugin: "plugin record\n"}
			target := bundlePath(dir, stamp)
			temporary := filepath.Join(dir, ".musicfs-log-"+filepath.Base(target)+".tmp-123")
			bundleFixture(t, temporary, entries)
			switch scenario {
			case "published", "truncated":
				bundleFixture(t, target, entries)
			case "partial-published":
				bundleFixture(t, target, map[string]string{server: entries[server]})
			case "different-published":
				bundleFixture(t, target, map[string]string{server: entries[server], plugin: "different plugin record\n"})
			case "sources":
				for name, data := range entries {
					if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario == "truncated" {
				data := []byte(contents(t, temporary))
				if err := os.WriteFile(temporary, data[:len(data)-4], 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := []byte(contents(t, temporary))
			m := newManager(t, dir, io.Discard)
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			if scenario == "published" || scenario == "sources" {
				assertMissing(t, temporary)
				published := readBundle(t, target)
				if len(published) != 2 || published[server] != entries[server] || published[plugin] != entries[plugin] {
					t.Fatal("removing the temporary bundle lost a record")
				}
			} else if !bytes.Equal([]byte(contents(t, temporary)), before) {
				t.Fatal("incomplete or unrecoverable temporary bundle changed")
			}
		})
	}
}

func TestArchivePreservesRecentLegacyGzipWithoutCreatingNewOnes(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Now().UTC().Add(-time.Hour)
	legacy := archiveFixture(t, dir, "server", stamp, "legacy marker") + ".gz"
	if err := os.Rename(legacy[:len(legacy)-3], legacy); err != nil {
		t.Fatal(err)
	}
	source := archiveFixture(t, dir, "server", stamp.Add(time.Millisecond), "new record\n")
	newManager(t, dir, io.Discard)
	if contents(t, legacy) != "legacy marker" {
		t.Fatal("recent legacy archive was changed")
	}
	assertMissing(t, source)
	assertMissing(t, source+".gz")
	if entries := readBundle(t, bundlePath(dir, stamp)); len(entries) != 1 || entries[filepath.Base(source)] != "new record\n" {
		t.Fatal("new log was not added to the daily bundle")
	}
}

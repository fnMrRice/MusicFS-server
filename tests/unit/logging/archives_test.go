package logging_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

func archiveFixture(t *testing.T, directory, id string, age time.Duration, data string) string {
	t.Helper()
	name := id + "-" + time.Now().UTC().Add(-age).Format("2006-01-02T15-04-05.000") + ".log"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gzipFixture(t *testing.T, path, data string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("file should have been removed: %s (%v)", path, err)
	}
}

func TestArchiveStartupRecoveryAndInactivePluginRetention(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(plugins, 0700); err != nil {
		t.Fatal(err)
	}
	source := archiveFixture(t, dir, "server", time.Hour, "uncompressed source\n")
	partial := filepath.Join(dir, ".musicfs-log-"+filepath.Base(source)+".gz.tmp-123")
	if err := os.WriteFile(partial, []byte("interrupted gzip"), 0600); err != nil {
		t.Fatal(err)
	}
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
	for path, age := range map[string]time.Duration{inactive: 5 * 24 * time.Hour, expired: 31 * 24 * time.Hour, unknown: 40 * 24 * time.Hour} {
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	newManager(t, dir, io.Discard)
	assertMissing(t, source)
	assertMissing(t, partial)
	assertMissing(t, inactive)
	assertMissing(t, expired)
	if contents(t, current) != "inactive plugin record\n" || contents(t, unknown) != "inactive plugin record\n" {
		t.Fatal("current or unknown file changed")
	}
	if data := waitArchive(t, source+".gz"); data != "uncompressed source\n" {
		t.Fatal("interrupted compression lost its source")
	}
	if data := waitArchive(t, filepath.Join(plugins, inactiveID+"-*.log.gz")); data != "inactive plugin record\n" {
		t.Fatal("inactive plugin log was not archived")
	}
}

func TestArchiveExistingGzipRecoveryPreservesConflicts(t *testing.T) {
	for _, scenario := range []string{"matching", "corrupt", "different", "longer"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			source := archiveFixture(t, dir, "server", time.Hour, "source record\n")
			target := source + ".gz"
			data := "source record\n"
			switch scenario {
			case "different":
				data = "other record\n"
			case "longer":
				data = strings.Repeat("important distinct archive\n", 5)
			}
			gzipFixture(t, target, data)
			if scenario == "corrupt" {
				if err := os.WriteFile(target, []byte("incomplete gzip"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			manager, err := logging.New(logging.Options{Directory: dir, Console: io.Discard})
			conflict := scenario == "different" || scenario == "longer"
			if conflict {
				if err == nil {
					manager.Close()
					t.Fatal("conflicting valid archive was accepted")
				}
				if contents(t, source) != "source record\n" || !bytes.Equal([]byte(contents(t, target)), before) {
					t.Fatal("conflict overwrote existing content")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			assertMissing(t, source)
			if data := waitArchive(t, target); data != "source record\n" {
				t.Fatal("recovered archive content changed")
			}
		})
	}
}

func TestArchiveTemporaryFilesRequireAnotherCompleteCopy(t *testing.T) {
	dir := t.TempDir()
	source := archiveFixture(t, dir, "server", time.Hour, "retained record\n")
	gzipFixture(t, source+".gz", "retained record\n")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	redundant := filepath.Join(dir, ".musicfs-log-"+filepath.Base(source)+".gz.tmp-123")
	sole := filepath.Join(dir, ".musicfs-log-server-2000-01-01T00-00-00.000.log.gz.tmp-456")
	unknown := filepath.Join(dir, ".musicfs-log-unrelated-2000-01-01T00-00-00.000.log.gz.tmp-789")
	for _, path := range []string{redundant, sole, unknown} {
		if err := os.WriteFile(path, []byte("interrupted bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	newManager(t, dir, io.Discard)
	assertMissing(t, redundant)
	if contents(t, sole) != "interrupted bytes" || contents(t, unknown) != "interrupted bytes" {
		t.Fatal("unrecoverable or unknown temporary content was removed")
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
	paths, err := filepath.Glob(filepath.Join(dir, "server-*.log.gz"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("close did not complete compression: %v %v", paths, err)
	}
	file, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || !bytes.Contains(data, []byte("archive before close")) {
		t.Fatalf("closed archive is incomplete: %v", err)
	}
	assertMissing(t, strings.TrimSuffix(paths[0], ".gz"))
}

func TestArchiveCleanupIgnoresSymlinks(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte("outside marker"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "server-2000-01-01T00-00-00.000.log.gz")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}
	newManager(t, dir, io.Discard)
	if _, err := os.Lstat(link); err != nil || contents(t, outside) != "outside marker" {
		t.Fatal("cleanup touched a symbolic link or its target")
	}
}

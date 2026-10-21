package plugindb_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

func TestReadLegacyScopeLeavesOldServerStateIntact(t *testing.T) {
	ctx := context.Background()
	values := []storedValue{
		{"musicfs.localfiles\x00local", plugindb.ConfigurationKey, 7, `{"name":"My library"}`},
		{"musicfs.localfiles\x00other", plugindb.ConfigurationKey, 2, `{"name":"Other library"}`},
	}
	path := legacyDatabase(t, values)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	read := func(wantRevision int64) {
		t.Helper()
		value, err := state.Read(ctx, path, "musicfs.plugin.source.localfiles", instanceid.Canonical("local"), plugindb.ConfigurationKey)
		if err != nil || value.Revision != wantRevision || string(value.Value) != values[0].value {
			t.Fatalf("legacy read changed or lost its scope: %+v %v", value, err)
		}
	}
	read(7)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("configuration read changed the database file: %v", err)
	}
	if got := readStoredValues(t, path); !reflect.DeepEqual(values, got) {
		t.Fatalf("configuration read migrated state before the server lock: %+v", got)
	}
	// Simulate a legacy server still updating its original scope while a new
	// process loads configuration, before the new process acquires its lock.
	writer, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`UPDATE plugin_values SET revision=8 WHERE scope=? AND key=?`, values[0].scope, values[0].key); err != nil {
		t.Fatal(err)
	}
	read(8)
	runtime, err := state.Open(ctx, path, "musicfs.plugin.source.localfiles", instanceid.Canonical("local"))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	value, err := runtime.Get(ctx, plugindb.ConfigurationKey)
	if err != nil || value.Revision != 8 {
		t.Fatalf("later runtime migration lost the latest legacy revision: %+v %v", value, err)
	}
}

func TestReadRejectsAmbiguousScopesWithoutWriting(t *testing.T) {
	values := []storedValue{
		{"musicfs.localfiles\x00local", "legacy-only", 7, `null`},
		{"musicfs.plugin.source.localfiles\x00" + instanceid.Canonical("local"), "current-only", 11, `null`},
	}
	path := legacyDatabase(t, values)
	before := readStoredValues(t, path)
	_, err := state.Read(context.Background(), path, "musicfs.plugin.source.localfiles", instanceid.Canonical("local"), "legacy-only")
	var detail *domain.Error
	if !errors.As(err, &detail) || detail.Code != "CONFLICT" {
		t.Fatalf("expected explicit conflict instead of choosing a scope: %v", err)
	}
	if after := readStoredValues(t, path); !reflect.DeepEqual(before, after) {
		t.Fatalf("conflict changed persisted state: %+v", after)
	}
}

func TestReadDoesNotCreateMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "plugins.sqlite")
	_, err := state.Read(context.Background(), path, "musicfs.plugin.source.localfiles", instanceid.Canonical("local"), plugindb.ConfigurationKey)
	if err == nil {
		t.Fatal("missing database was silently accepted")
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read created the database directory: %v", err)
	}
}

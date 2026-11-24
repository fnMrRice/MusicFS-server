package plugindb_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

func TestCurrentScopesPreserveStoredSettingsDuringStartup(t *testing.T) {
	ctx := context.Background()
	values := []storedValue{
		{"musicfs.plugin.source.localfiles\x00" + instanceid.Canonical("local"), plugindb.ConfigurationKey, 7, `{"name":"My library"}`},
		{"musicfs.plugin.source.localfiles\x00" + instanceid.Canonical("other"), plugindb.ConfigurationKey, 2, `{"name":"Other library"}`},
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
			t.Fatalf("configuration read changed or lost its scope: %+v %v", value, err)
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
	// 配置预读发生在加锁之前，应能在下次查询看到其他进程刚写入的修订。
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
		t.Fatalf("runtime lost the latest revision: %+v %v", value, err)
	}
	var version int
	if err := writer.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("validated state schema was not persisted: version=%d err=%v", version, err)
	}
}

func TestObsoleteScopesRequireExplicitUpgrade(t *testing.T) {
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
	if store, openErr := state.Open(t.Context(), path, "musicfs.plugin.source.localfiles", instanceid.Canonical("local")); openErr == nil {
		store.Close()
		t.Fatal("obsolete scopes were accepted by the writer")
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

func TestScopeFormatMatchesDatabaseUpgradeCheck(t *testing.T) {
	const id = "b1548e96-3081-5c9c-8376-bf290e369d51"
	for _, tc := range []struct{ plugin, instance string }{
		{"musicfs.localfiles", id},
		{"musicfs.plugin.source.localfiles", "local"},
		{"musicfs.plugin.source.localfiles", strings.ToUpper(id)},
		{"musicfs.plugin.source.localfiles", "00000000-0000-0000-0000-000000000000"},
		{"musicfs.plugin.test\x00part", id},
	} {
		path := legacyDatabase(t, []storedValue{{tc.plugin + "\x00" + tc.instance, "value", 1, "null"}})
		if db, err := state.Open(t.Context(), path, tc.plugin, tc.instance); err == nil {
			db.Close()
			t.Fatal("invalid input scope was accepted")
		}
		_, err := state.Read(t.Context(), path, "musicfs.plugin.test", id, "value")
		var detail *domain.Error
		if !errors.As(err, &detail) || detail.Code != "CONFLICT" {
			t.Fatalf("invalid persisted scope passed the schema check: %v", err)
		}
	}
}

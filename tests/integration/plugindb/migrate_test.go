package plugindb_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

type storedValue struct {
	scope, key string
	revision   int64
	value      string
}

func legacyDatabase(t *testing.T, values []storedValue) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugins.sqlite")
	db, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE plugin_values(scope TEXT NOT NULL, key TEXT NOT NULL, revision INTEGER NOT NULL, value BLOB NOT NULL, PRIMARY KEY(scope,key))`); err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if _, err := db.Exec(`INSERT INTO plugin_values(scope,key,revision,value) VALUES(?,?,?,?)`, value.scope, value.key, value.revision, []byte(value.value)); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func readStoredValues(t *testing.T, path string) []storedValue {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT scope,key,revision,value FROM plugin_values ORDER BY scope,key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	values := []storedValue{}
	for rows.Next() {
		var value storedValue
		if err := rows.Scan(&value.scope, &value.key, &value.revision, &value.value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func TestLegacyPluginScopesPreserveSettingsAndInstances(t *testing.T) {
	ctx := context.Background()
	mappings := []struct{ old, current, instance string }{
		{"musicfs.localfiles", "musicfs.plugin.source.localfiles", "primary"},
		{"musicfs.localfiles", "musicfs.plugin.source.localfiles", "secondary"},
		{"musicfs.netease", "musicfs.plugin.source.netease", "online"},
		{"musicfs.qqmusic", "musicfs.plugin.source.qqmusic", "online"},
		{"musicfs.applemusic", "musicfs.plugin.source.applemusic", "online"},
		{"musicfs.audio.ffmpeg", "musicfs.plugin.decoder.ffmpeg", "dotted"},
		{"musicfs.audioffmpeg", "musicfs.plugin.decoder.ffmpeg", "compact"},
		{"musicfs.ncm", "musicfs.plugin.decoder.ncm", "decrypt"},
		{"musicfs.scraper.local", "musicfs.scraper.local", "lyrics"},
		{"musicfs.localfiles.extra", "musicfs.localfiles.extra", "other"},
	}
	keys := []string{plugindb.ConfigurationKey, "host-default-read-directories", "worker-cache"}
	values := []storedValue{}
	for index, mapping := range mappings {
		for _, key := range keys {
			values = append(values, storedValue{mapping.old + "\x00" + mapping.instance, key, int64(index + 3), `{"preserve":"` + mapping.instance + `"}`})
		}
	}
	path := legacyDatabase(t, values)
	first, err := plugindb.Open(ctx, path, mappings[0].current, mappings[0].instance)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	expected := append([]storedValue{}, values...)
	for index, mapping := range mappings {
		for key := range keys {
			expected[index*len(keys)+key].scope = mapping.current + "\x00" + instanceid.Canonical(mapping.instance)
		}
	}
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].scope != expected[j].scope {
			return expected[i].scope < expected[j].scope
		}
		return expected[i].key < expected[j].key
	})
	if got := readStoredValues(t, path); !reflect.DeepEqual(expected, got) {
		t.Fatalf("opening one instance did not migrate all scopes intact: expected=%+v got=%+v", expected, got)
	}
	for index, mapping := range mappings {
		db, err := plugindb.Open(ctx, path, mapping.current, mapping.instance)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			value, err := db.Get(ctx, key)
			if err != nil || value.Revision != int64(index+3) || string(value.Value) != `{"preserve":"`+mapping.instance+`"}` {
				t.Fatalf("scope %q key %q lost state: %+v %v", mapping.current, key, value, err)
			}
		}
		db.Close()
	}
	after := readStoredValues(t, path)
	if len(after) != len(values) {
		t.Fatalf("migration changed row count: %d != %d", len(after), len(values))
	}
	db, err := plugindb.Open(ctx, path, mappings[0].old, mappings[0].instance)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CompareAndSet(ctx, pluginproto.StateRequest{Key: "worker-cache", Revision: 3, Value: []byte(`{"updated":true}`)})
	db.Close()
	if err != nil {
		t.Fatalf("legacy alias failed to write canonical scope: %v", err)
	}
	reopened, err := plugindb.Open(ctx, path, mappings[0].current, mappings[0].instance)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	value, err := reopened.Get(ctx, "worker-cache")
	if err != nil || value.Revision != 4 || string(value.Value) != `{"updated":true}` {
		t.Fatalf("canonical scope lost alias update: %+v %v", value, err)
	}
}

func TestPluginScopeConflictsLeaveEntireDatabaseUnchanged(t *testing.T) {
	for _, pair := range [][2]string{
		{"musicfs.localfiles", "musicfs.plugin.source.localfiles"},
		{"musicfs.audio.ffmpeg", "musicfs.audioffmpeg"},
	} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			values := []storedValue{
				{pair[0] + "\x00same", "old-only", 7, `{"old":true}`},
				{pair[1] + "\x00same", "current-only", 11, `{"current":true}`},
				{"musicfs.ncm\x00another", "unrelated", 2, `null`},
			}
			path := legacyDatabase(t, values)
			before := readStoredValues(t, path)
			db, err := plugindb.Open(context.Background(), path, "musicfs.plugin.decoder.ncm", "another")
			if db != nil {
				db.Close()
				t.Fatal("conflicting database returned a usable store")
			}
			var detail *domain.Error
			if !errors.As(err, &detail) || detail.Code != "CONFLICT" {
				t.Fatalf("expected explicit scope conflict: %v", err)
			}
			if after := readStoredValues(t, path); !reflect.DeepEqual(before, after) {
				t.Fatalf("conflict changed database: before=%+v after=%+v", before, after)
			}
		})
	}
}

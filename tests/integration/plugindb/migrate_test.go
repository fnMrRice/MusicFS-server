package plugindb_test

import (
	"database/sql"
	"path/filepath"
	"testing"
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

package migrations_test

import (
	"fmt"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

func migrationFiles(contents map[string]string) fstest.MapFS {
	files := fstest.MapFS{}
	for name, body := range contents {
		files[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return files
}

func TestLoadFromPreservesHistoricalSQL(t *testing.T) {
	for name, test := range map[string]struct {
		files map[string]string
		want  []migrations.Migration
	}{
		"standalone": {
			files: map[string]string{
				"001_first.sql":  "SELECT 1;\n",
				"002_second.sql": "-- Existing comment\nSELECT 2;\n",
			},
			want: []migrations.Migration{
				{Version: 1, SQL: "SELECT 1;\n"},
				{Version: 2, SQL: "-- Existing comment\nSELECT 2;\n"},
			},
		},
		"batch and standalone with CRLF": {
			files: map[string]string{
				"002_batch_001_002.sql": "-- migration: 001_first.sql\r\nSELECT 1;\r\n-- migration: 002_second.sql\r\nSELECT 2;\r\n",
				"003_latest.sql":        "SELECT 3;\r\n",
			},
			want: []migrations.Migration{
				{Version: 1, SQL: "SELECT 1;\n"},
				{Version: 2, SQL: "SELECT 2;\n"},
				{Version: 3, SQL: "SELECT 3;\n"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := migrations.LoadFrom(migrationFiles(test.files))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("migration history = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestLoadFromOrdersVersionsNumerically(t *testing.T) {
	files := fstest.MapFS{}
	var want []migrations.Migration
	for version := 1; version <= 10; version++ {
		body := fmt.Sprintf("SELECT %d;\n", version)
		files[fmt.Sprintf("%d_history.sql", version)] = &fstest.MapFile{Data: []byte(body)}
		want = append(want, migrations.Migration{Version: version, SQL: body})
	}
	got, err := migrations.LoadFrom(files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("migration history = %#v, want numeric version order %#v", got, want)
	}
}

func TestLoadFromRejectsInvalidHistory(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"empty history": {},
		"missing first version": {
			"002_second.sql": "SELECT 2;\n",
		},
		"missing middle version": {
			"001_first.sql": "SELECT 1;\n",
			"003_third.sql": "SELECT 3;\n",
		},
		"duplicate version": {
			"001_first.sql": "SELECT 1;\n",
			"001_other.sql": "SELECT 2;\n",
		},
		"empty standalone SQL": {
			"001_first.sql": " \r\n\t",
		},
		"empty batch SQL": {
			"002_batch_001_002.sql": "-- migration: 001_first.sql\n \n-- migration: 002_second.sql\nSELECT 2;\n",
		},
		"SQL before first marker": {
			"001_batch_001_001.sql": "SELECT 0;\n-- migration: 001_first.sql\nSELECT 1;\n",
		},
		"invalid marker filename": {
			"001_batch_001_001.sql": "-- migration: invalid.sql\nSELECT 1;\n",
		},
		"unterminated marker": {
			"001_batch_001_001.sql": "-- migration: 001_first.sql",
		},
		"batch end mismatch": {
			"003_batch_001_003.sql": "-- migration: 001_first.sql\nSELECT 1;\n-- migration: 002_second.sql\nSELECT 2;\n",
		},
		"batch segment gap": {
			"003_batch_001_003.sql": "-- migration: 001_first.sql\nSELECT 1;\n-- migration: 003_third.sql\nSELECT 3;\n",
		},
		"batch segment order": {
			"003_batch_001_003.sql": "-- migration: 002_second.sql\nSELECT 2;\n-- migration: 001_first.sql\nSELECT 1;\n-- migration: 003_third.sql\nSELECT 3;\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := migrations.LoadFrom(migrationFiles(files)); err == nil {
				t.Fatalf("invalid migration history was accepted: %#v", got)
			}
		})
	}
}

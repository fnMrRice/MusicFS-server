package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSchemaVersionUsesTargetMigrationHistory(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "src", "storage", "migrations")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	var batch strings.Builder
	for version := 1; version <= 10; version++ {
		fmt.Fprintf(&batch, "-- migration: %03d_history.sql\nSELECT %d;\n", version, version)
	}
	for name, body := range map[string]string{
		"010_batch_001_010.sql": batch.String(),
		"011_latest.sql":        "SELECT 11;\n",
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	version, err := (builder{root: root}).schemaVersion()
	if err != nil || version != 11 {
		t.Fatalf("release schema version = %d, want 11 from target history: %v", version, err)
	}
}

func TestReleaseSchemaVersionRejectsIncompleteHistory(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"empty": {},
		"missing version": {
			"001_first.sql": "SELECT 1;\n",
			"003_third.sql": "SELECT 3;\n",
		},
		"missing batch segment": {
			"003_batch_001_003.sql": "-- migration: 001_first.sql\nSELECT 1;\n-- migration: 003_third.sql\nSELECT 3;\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "src", "storage", "migrations")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			for filename, body := range files {
				if err := os.WriteFile(filepath.Join(directory, filename), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if version, err := (builder{root: root}).schemaVersion(); err == nil {
				t.Fatalf("release accepted incomplete migration history at version %d", version)
			}
		})
	}
}

package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRootOptionsBindRecoveryReconciliationAndLyricsTriggers(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	full := testkit.ScanPlan("flat", "deep")
	flat := scopeObservation("flat-child", "flat", "sub/song.mp3")
	deep := scopeObservation("deep-child", "deep", "sub/song.mp3")
	publishScope(t, s, full, flat, deep)
	limited := full
	limited.RootOptions = map[string]pluginproto.ImportOptions{"flat": {ImportLyrics: true}}
	job := publishScope(t, s, limited, scopeObservation("flat-root", "flat", "song.mp3"), scopeObservation("deep-root", "deep", "song.mp3"))
	tracks := tracksByName(t, s)
	if tracks["flat-child"].Availability != "available" || tracks["deep-child"].Availability != "missing" {
		t.Fatal("scan cleanup ignored independent root coverage")
	}
	var roots []string
	rows, err := s.DB.QueryContext(ctx, "SELECT root_id FROM metadata_automation WHERE scan_job_id=? AND reason='importLyrics'", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var root string
		if err := rows.Scan(&root); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != "flat" {
		t.Fatal("lyrics import leaked across root settings", roots)
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || value["effectiveRootOptions"] == nil {
		t.Fatal("job hid the per-root effective configuration")
	}
}

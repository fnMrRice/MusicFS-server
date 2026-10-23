package plugins_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMetadataRefreshIsOnlyASingleScanOverride(t *testing.T) {
	var options pluginproto.ScanOverrides
	if err := json.Unmarshal([]byte(`{"refreshMetadata":true,"recursive":false,"importArtwork":false}`), &options); err != nil {
		t.Fatal(err)
	}
	if !options.RefreshMetadata || options.Recursive == nil || *options.Recursive || options.ImportArtwork == nil || *options.ImportArtwork {
		t.Fatalf("scan overrides changed: %+v", options)
	}
	for _, raw := range []string{`null`, `[]`, `{"refreshMetadata":null}`, `{"refreshMetadata":1}`, `{"refreshMetadata":"true"}`, `{"unknown":true}`} {
		if err := json.Unmarshal([]byte(raw), &options); err == nil {
			t.Fatalf("invalid scan override accepted: %s", raw)
		}
	}
	var defaults pluginproto.ImportOverrides
	if err := json.Unmarshal([]byte(`{"refreshMetadata":true}`), &defaults); err == nil {
		t.Fatal("one-shot refresh accepted as saved defaults")
	}
}

func TestMetadataRefreshBindsPlanAndRequiresOneFullRoot(t *testing.T) {
	plan := testkit.ScanPlan("music")
	before, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	plan.RefreshMetadata = true
	after, err := plan.Digest()
	if err != nil || before == after {
		t.Fatal("refresh mode was not bound to scan identity", err)
	}
	plan.Mode = "incremental"
	if _, err := plan.Canonical(); err == nil {
		t.Fatal("partial refresh accepted")
	}
	plan = testkit.ScanPlan("one", "two")
	plan.RefreshMetadata = true
	if _, err := plan.Canonical(); err == nil {
		t.Fatal("refresh escaped a single directory")
	}
}

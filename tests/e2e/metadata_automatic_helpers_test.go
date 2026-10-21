package e2e_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func automaticScan(t *testing.T, c *client, options map[string]any, key string) (string, map[string]library.Track) {
	t.Helper()
	input := providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51")
	input["options"] = options
	code, raw, _ := c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", input, key)
	var accepted struct {
		JobID string `json:"jobId"`
	}
	if code != 202 || json.Unmarshal(raw, &accepted) != nil {
		t.Fatalf("scan: %d %s", code, raw)
	}
	waitScanOptions(t, c, accepted.JobID)
	code, raw, _ = c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page library.Page
	if code != 200 || json.Unmarshal(raw, &page) != nil {
		t.Fatalf("sources: %d %s", code, raw)
	}
	tracks := map[string]library.Track{}
	for _, track := range page.Items {
		tracks[track.DisplayName] = track
	}
	return accepted.JobID, tracks
}

func waitAutomatic(t *testing.T, c *client, scan string, count int) []enrichment.AutomationResult {
	t.Helper()
	for deadline := time.Now().Add(35 * time.Second); ; {
		items := []enrichment.AutomationResult{}
		cursor := ""
		for {
			code, raw, _ := c.request(t, "GET", "/api/v1/scan-jobs/"+scan+"/metadata-results?limit=2&cursor="+cursor, nil, "")
			var page enrichment.AutomationPage
			if code != 200 || json.Unmarshal(raw, &page) != nil {
				t.Fatalf("automatic results: %d %s", code, raw)
			}
			testkit.ValidateHTTP(t, "MetadataAutomationPage", raw)
			items = append(items, page.Items...)
			if page.NextCursor == nil {
				break
			}
			if *page.NextCursor == cursor || len(items) > 100 {
				t.Fatal("unbounded result pagination")
			}
			cursor = *page.NextCursor
		}
		complete := len(items) == count
		for _, item := range items {
			if item.State == "queued" || item.State == "preparing" || item.State == "running" {
				complete = false
			}
		}
		if complete {
			return items
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic timeout, wanted %d: %+v", count, items)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func automaticResult(t *testing.T, items []enrichment.AutomationResult, source, reason string) enrichment.AutomationResult {
	t.Helper()
	for _, item := range items {
		if item.SourceTrackID == source && item.Reason == reason {
			return item
		}
	}
	t.Fatalf("missing %s result for %s", reason, source)
	return enrichment.AutomationResult{}
}

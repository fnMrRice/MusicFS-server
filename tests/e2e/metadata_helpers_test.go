package e2e_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func metadataRequest(ids []string, fields ...string) map[string]any {
	return map[string]any{"sourceTrackIds": ids, "fields": fields, "instanceIds": []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}}
}

func metadataJob(t *testing.T, c *client, input map[string]any, key string) enrichment.Job {
	t.Helper()
	code, raw, _ := c.request(t, "POST", "/api/v1/metadata-jobs", input, key)
	var accepted struct {
		JobID  string `json:"jobId"`
		Merged bool   `json:"merged"`
	}
	if code != 202 || json.Unmarshal(raw, &accepted) != nil {
		t.Fatalf("metadata submit: %d %s", code, raw)
	}
	for deadline := time.Now().Add(40 * time.Second); ; {
		code, raw, _ = c.request(t, "GET", "/api/v1/metadata-jobs/"+accepted.JobID, nil, "")
		var job enrichment.Job
		if code != 200 || json.Unmarshal(raw, &job) != nil {
			t.Fatalf("metadata get: %d %s", code, raw)
		}
		testkit.ValidateHTTP(t, "MetadataJob", raw)
		for _, private := range [][]byte{[]byte(`"resourceRef"`), []byte(`"resourceVersion"`), []byte(`"generation"`), []byte(`"relativePath"`), []byte(`"contentRef"`)} {
			if bytes.Contains(raw, private) {
				t.Fatal("metadata control response leaked private resource information")
			}
		}
		if job.State != "queued" && job.State != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("metadata job timeout: %s", raw)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func applyMetadata(t *testing.T, c *client, candidate string, revision int64, key string) mediastore.Selection {
	t.Helper()
	code, raw, _ := c.request(t, "POST", "/api/v1/metadata-candidates/"+candidate+"/apply", map[string]any{"expectedRevision": revision}, key)
	var out mediastore.Selection
	if code != 200 || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("metadata apply: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "MediaSelection", raw)
	return out
}

func metadataSelection(t *testing.T, c *client, track string) mediastore.Selection {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks/"+track+"/media-selection", nil, "")
	var out mediastore.Selection
	if code != 200 || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("metadata selection: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "MediaSelection", raw)
	return out
}

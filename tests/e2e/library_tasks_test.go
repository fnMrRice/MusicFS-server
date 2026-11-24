package e2e_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

func TestLibraryTasksExecuteThroughHTTP(t *testing.T) {
	cfg, base := yamlServiceConfig(t)
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	revision := albumRequest[struct{ Revision int }](t, c, "GET", "/matching/revision", nil, "", 200, "MatchingRevision").Revision
	for _, tc := range []struct {
		path, kind, status string
		input              any
	}{
		{"/tasks/album-scrape", tasks.AlbumAutoScrape, "succeeded", map[string]any{"q": "empty-album-filter"}},
		{"/tasks/bulk-merge", tasks.BulkMerge, "succeeded", map[string]any{"mode": "title_artist", "expectedRevision": revision}},
		{"/tasks/bulk-merge", tasks.BulkMerge, "failed", map[string]any{"mode": "score", "minimumScore": 75, "expectedRevision": revision + 1}},
	} {
		key := tc.kind + "-" + tc.status
		task := albumRequest[tasks.Task](t, c, "POST", tc.path, tc.input, key, 202, "Task")
		deadline := time.Now().Add(8 * time.Second)
		for task.Status == "queued" || task.Status == "running" {
			if time.Now().After(deadline) {
				t.Fatalf("task did not finish: %+v", task)
			}
			time.Sleep(15 * time.Millisecond)
			task = albumRequest[tasks.Task](t, c, "GET", "/tasks/"+task.ID, nil, "", 200, "Task")
		}
		if task.Kind != tc.kind || task.Status != tc.status || (tc.status == "failed" && (task.Error == nil || task.Error.Code != "CONFLICT")) {
			t.Fatalf("unexpected outcome: %+v", task)
		}
		replayed := albumRequest[tasks.Task](t, c, "POST", tc.path, tc.input, key, 202, "Task")
		if replayed.ID != task.ID || replayed.Status != task.Status {
			t.Fatalf("replay returned a different task: %+v", replayed)
		}
	}
	for _, tc := range []struct {
		path string
		body any
	}{
		{"/tasks/album-scrape", json.RawMessage("null")},
		{"/tasks/album-scrape", map[string]any{"providerInstanceId": nil}},
		{"/tasks/album-scrape", map[string]any{"ownerId": "another"}},
		{"/tasks/bulk-merge", map[string]any{"mode": "title_artist"}},
		{"/tasks/bulk-merge", map[string]any{"expectedRevision": 0, "mode": "score"}},
	} {
		albumRequest[any](t, c, "POST", tc.path, tc.body, "rejected-task", 400, "Error")
	}
	page := albumRequest[tasks.Page](t, c, "GET", "/tasks", nil, "", 200, "TaskPage")
	if page.ActiveCount != 0 || len(page.Items) != 3 {
		t.Fatalf("unexpected queue after replay and validation: %+v", page)
	}
}

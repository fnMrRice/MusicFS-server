package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/tasks"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestTaskQueueHTTPReadsPersistedRefreshesAndCancelsQueuedTask(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	service := tasks.New(store, nil, nil, nil)
	defer service.Close()
	server.RegisterTasks(service)
	plan := testkit.ScanPlan("316664dc-7106-5acd-8df7-88c9d0d9baef")
	if _, _, err := store.Enqueue(context.Background(), "admin", "316664dc-7106-5acd-8df7-88c9d0d9baef", plan, "ordinary-scan"); err != nil {
		t.Fatal(err)
	}
	plan.RefreshMetadata = true
	job, _, err := store.Enqueue(context.Background(), "admin", "316664dc-7106-5acd-8df7-88c9d0d9baef", plan, "explicit-refresh")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		return w
	}
	w := request(http.MethodGet, "/api/v1/tasks", "")
	testkit.ValidateHTTP(t, "TaskPage", w.Body.Bytes())
	var page tasks.Page
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != job.ID || page.ActiveCount != 1 {
		t.Fatal("queue included automatic scan or omitted refresh", page)
	}
	for range 2 {
		w = request(http.MethodPost, "/api/v1/tasks/"+job.ID+"/cancel", "{}")
		testkit.ValidateHTTP(t, "Task", w.Body.Bytes())
		var task tasks.Task
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.Status != "cancelled" || !task.CancelRequested || task.Cancellable {
			t.Fatal("queued cancellation was not durable and idempotent", task)
		}
	}
	w = request(http.MethodGet, "/api/v1/tasks", "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.ActiveCount != 0 {
		t.Fatal("cancelled task remained active", page)
	}
}

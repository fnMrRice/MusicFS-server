package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type taskHTTPService struct{ items []tasks.Task }

func (s *taskHTTPService) List(_ context.Context, owner, _ string, _ int) (tasks.Page, error) {
	page := tasks.Page{Items: []tasks.Task{}}
	for _, task := range s.items {
		if owner == "" || task.OwnerID == owner {
			page.Items = append(page.Items, task)
			page.ActiveCount++
		}
	}
	return page, nil
}
func (s *taskHTTPService) Get(_ context.Context, owner, id string) (tasks.Task, error) {
	for _, task := range s.items {
		if task.ID == id && (owner == "" || owner == task.OwnerID) {
			return task, nil
		}
	}
	return tasks.Task{}, domain.Fail("NOT_FOUND", "Task not found")
}
func (s *taskHTTPService) Cancel(ctx context.Context, owner, id string) (tasks.Task, error) {
	task, err := s.Get(ctx, owner, id)
	if err == nil {
		task.Status = "cancelled"
		task.Phase = "finished"
		task.CancelRequested = true
		task.Cancellable = false
	}
	return task, err
}

func TestTaskQueueHTTPEnforcesScopeAndCancellationCSRF(t *testing.T) {
	repository := &memoryAuth{sessions: map[string]auth.Session{}}
	authentication := auth.New(repository, testToken, []string{origin}, time.Hour)
	server := api.New(api.Options{Auth: authentication})
	service := &taskHTTPService{}
	cookies := map[string]*http.Cookie{}
	for _, owner := range []string{"listener", "other", "admin"} {
		role := "user"
		if owner == "admin" {
			role = "admin"
		}
		token := domain.NewID()
		if err := repository.SaveAuth(context.Background(), auth.Session{ID: auth.Digest(token), User: auth.User{ID: owner, Role: role}, CSRFToken: owner + "-csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		cookies[owner] = &http.Cookie{Name: api.CookieName, Value: token}
		if owner != "admin" {
			letter := "a"
			if owner == "other" {
				letter = "b"
			}
			service.items = append(service.items, tasks.Task{ID: strings.Repeat(letter, 48), Kind: "metadataRefresh", OwnerID: owner, OwnerName: owner, ProviderInstanceID: "316664dc-7106-5acd-8df7-88c9d0d9baef", ProviderName: "Local music", RootIDs: []string{"root"}, Roots: []tasks.Root{{ID: "root", Name: "Music"}}, Status: "queued", Phase: "queued", Cancellable: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
		}
	}
	server.RegisterTasks(service)
	call := func(method, path, owner, csrf, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if cookies[owner] != nil {
			r.AddCookie(cookies[owner])
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	for _, test := range []struct {
		path, owner string
		status      int
	}{
		{"/api/v1/tasks", "", 401}, {"/api/v1/tasks?scope=all", "listener", 403},
		{"/api/v1/tasks?scope=invalid", "admin", 400}, {"/api/v1/tasks?scope=mine&scope=all", "admin", 400},
		{"/api/v1/tasks?limit=0", "admin", 400}, {"/api/v1/tasks?limit=201", "admin", 400}, {"/api/v1/tasks?ownerId=other", "listener", 400},
		{"/api/v1/tasks?limit=", "admin", 400}, {"/api/v1/tasks?scope=%ZZ", "admin", 400}, {"/api/v1/tasks?scope=mine;scope=all", "admin", 400},
		{"/api/v1/tasks/" + service.items[1].ID, "listener", 404},
	} {
		if w := call(http.MethodGet, test.path, test.owner, "", ""); w.Code != test.status {
			t.Fatalf("%s: HTTP %d: %s", test.path, w.Code, w.Body.String())
		}
	}
	for _, test := range []struct {
		path, owner string
		count       int
	}{{"/api/v1/tasks", "listener", 1}, {"/api/v1/tasks?scope=mine", "admin", 0}, {"/api/v1/tasks?scope=all", "admin", 2}} {
		w := call(http.MethodGet, test.path, test.owner, "", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "TaskPage", w.Body.Bytes())
		var page tasks.Page
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != test.count || page.ActiveCount != test.count {
			t.Fatal("scope leaked or omitted tasks", page)
		}
	}
	path := "/api/v1/tasks/" + service.items[1].ID + "/cancel"
	for _, test := range []struct {
		suffix, owner, csrf, body string
		status                    int
	}{{"", "other", "", "{}", 403}, {"", "listener", "listener-csrf", "{}", 404}, {"?scope=all", "listener", "listener-csrf", "{}", 403}, {"?scope=all", "admin", "admin-csrf", `{"unexpected":true}`, 400}, {"?scope=all", "admin", "admin-csrf", "{}", 200}} {
		w := call(http.MethodPost, path+test.suffix, test.owner, test.csrf, test.body)
		if w.Code != test.status {
			t.Fatalf("cancel HTTP %d: %s", w.Code, w.Body.String())
		}
		if w.Code == 200 {
			testkit.ValidateHTTP(t, "Task", w.Body.Bytes())
		} else {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
	}
}

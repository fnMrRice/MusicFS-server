package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

type observedQueue struct {
	taskHTTPService
	reads          atomic.Int32
	oldJobFinished atomic.Bool
}

func (s *observedQueue) List(ctx context.Context, owner, cursor string, limit int) (tasks.Page, error) {
	s.reads.Add(1)
	page, err := s.taskHTTPService.List(ctx, owner, cursor, limit)
	if s.oldJobFinished.Load() && (owner == "" || owner == "listener") {
		page.ActiveCount--
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = &page.Items[limit-1].ID
	}
	return page, err
}

func (s *observedQueue) Get(ctx context.Context, owner, id string) (tasks.Task, error) {
	task, err := s.taskHTTPService.Get(ctx, owner, id)
	if err == nil && id == "job-50" && s.oldJobFinished.Load() {
		task.Status, task.Phase = "succeeded", "finished"
	}
	return task, err
}

func TestControlTaskSubscriptionPushesPageCountAndHistoricalChanges(t *testing.T) {
	repo := &memoryAuth{sessions: map[string]auth.Session{}}
	s := api.New(api.Options{Auth: auth.New(repo, testToken, []string{origin}, time.Hour)})
	queue := &observedQueue{}
	for i := range 51 {
		queue.items = append(queue.items, tasks.Task{ID: fmt.Sprintf("job-%d", i), OwnerID: "listener"})
	}
	queue.items = append(queue.items, tasks.Task{ID: "other-job", OwnerID: "other"})
	s.RegisterTasks(queue)
	host := controlHTTP(t, s, nil)
	listener := connectControlUser(t, host, repo, "listener", "user")
	admin := connectControlUser(t, host, repo, "admin", "admin")
	subscribe := func(conn *websocket.Conn, id, scope string, want int) {
		t.Helper()
		response := controlCall(t, conn, id, "POST", "/tasks/subscription", map[string]string{"subscriptionId": id, "scope": scope}, "")
		if response.Status != want {
			t.Fatalf("subscribe %s returned %d", scope, response.Status)
		}
	}
	check := func(conn *websocket.Conn, id string, reset bool, count int, history ...string) {
		t.Helper()
		var snap struct {
			SubscriptionID string
			Page           tasks.Page
			Changed        []tasks.Task
			Reset          bool
		}
		for {
			event := readControl(t, conn, "event")
			if event.Event == "tasks.snapshot" {
				if err := json.Unmarshal(event.Data, &snap); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
		if snap.SubscriptionID != id || snap.Reset != reset || snap.Page.ActiveCount != count || len(snap.Page.Items) != min(count, 50) || len(snap.Changed) != len(history) {
			t.Fatalf("unexpected snapshot for %s: reset=%t count=%d rows=%d changes=%v", snap.SubscriptionID, snap.Reset, snap.Page.ActiveCount, len(snap.Page.Items), snap.Changed)
		}
		if count > 50 && (snap.Page.NextCursor == nil || *snap.Page.NextCursor != "job-49") {
			t.Fatal("invalid history cursor")
		}
		for i, taskID := range history {
			if snap.Changed[i].ID != taskID || snap.Changed[i].Status != "succeeded" {
				t.Fatal("wrong historical task", snap.Changed[i])
			}
		}
	}
	subscribe(listener, "denied-all", "all", 403)
	subscribe(listener, "listener-mine", "mine", 204)
	check(listener, "listener-mine", true, 51)
	subscribe(admin, "admin-mine", "mine", 204)
	check(admin, "admin-mine", true, 0)
	s.NotifyRefreshTask("other", "other-job")
	time.Sleep(1100 * time.Millisecond)
	if got := queue.reads.Load(); got != 2 {
		t.Fatalf("idle or out-of-scope change caused %d reads", got-2)
	}
	subscribe(admin, "admin-all", "all", 204)
	check(admin, "admin-all", true, 52)
	queue.oldJobFinished.Store(true)
	s.NotifyRefreshTask("listener", "job-50")
	check(listener, "listener-mine", false, 50, "job-50")
	check(admin, "admin-all", false, 51, "job-50")
	for i := range 201 {
		s.NotifyRefreshTask("listener", fmt.Sprintf("burst-%d", i))
	}
	check(listener, "listener-mine", true, 50)
	listener.CloseNow()
	reconnected := connectControlUser(t, host, repo, "listener", "user")
	subscribe(reconnected, "listener-reconnect", "mine", 204)
	check(reconnected, "listener-reconnect", true, 50)
}

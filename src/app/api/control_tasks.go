package api

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

const maxChangedTasks = 200

type taskSubscription struct {
	id      string
	owner   string // 仅已授权的 all 使用空 owner。
	reset   bool
	pending map[string]struct{}
}

type taskSnapshot struct {
	SubscriptionID string       `json:"subscriptionId"`
	Page           tasks.Page   `json:"page"`
	Changed        []tasks.Task `json:"changed"`
	Reset          bool         `json:"reset"`
}

func (c *controlConnection) configureTaskSubscription(r controlRequest) {
	var input struct {
		ID    string `json:"subscriptionId"`
		Scope string `json:"scope"`
	}
	if r.Method != "POST" || r.IdempotencyKey != "" || !controlDecode(r.Body, &input) || !safeRequestID.MatchString(input.ID) || input.Scope == "" {
		c.send(c.failure(r.ID, domain.Fail("INVALID_ARGUMENT", "Invalid task subscription")))
		return
	}
	owner, err := taskOwner(input.Scope, c.session)
	if err != nil {
		c.send(c.failure(r.ID, err))
		return
	}
	if c.server.control.tasks == nil {
		c.send(c.failure(r.ID, domain.Fail("UNSUPPORTED", "Task queue is unavailable")))
		return
	}
	c.server.control.mu.Lock()
	defer c.server.control.mu.Unlock()
	c.taskView = &taskSubscription{id: input.ID, owner: owner, reset: true}
	c.send(controlResponse{Type: "response", ID: r.ID, Status: 204})
	c.wakeTaskSnapshot()
}

func (s *taskSubscription) changed(id string) {
	if s.reset {
		return
	}
	s.pending[id] = struct{}{}
	if len(s.pending) > maxChangedTasks {
		s.reset = true
		clear(s.pending)
	}
}

func (c *controlConnection) wakeTaskSnapshot() {
	select {
	case c.taskWake <- struct{}{}:
	default:
	}
}

func (c *controlConnection) taskSnapshotLoop() {
	var next time.Time
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.taskWake:
		}
		// 只在收到提交通知后唤醒。连续变化合并，逐连接限制数据库读取频率。
		if delay := time.Until(next); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-c.ctx.Done():
				timer.Stop()
				return
			}
		}
		c.server.control.mu.Lock()
		view := c.taskView
		if view == nil || !view.reset && len(view.pending) == 0 {
			c.server.control.mu.Unlock()
			continue
		}
		reset, pending := view.reset, view.pending
		view.reset, view.pending = false, make(map[string]struct{})
		c.server.control.mu.Unlock()

		next = time.Now().Add(time.Second)
		if !c.publishTaskSnapshot(view, pending, reset) {
			c.cancel()
			return
		}
	}
}

func (c *controlConnection) publishTaskSnapshot(view *taskSubscription, pending map[string]struct{}, reset bool) bool {
	ctx, cancel := context.WithTimeout(c.ctx, c.server.options.ControlTimeout)
	defer cancel()
	service := c.server.control.tasks
	page, err := service.List(ctx, view.owner, "", 50)
	if err != nil {
		return false
	}
	for i := range page.Items {
		taskErrorID(&page.Items[i], view.id)
		delete(pending, page.Items[i].ID)
	}
	snapshot := taskSnapshot{SubscriptionID: view.id, Page: page, Changed: []tasks.Task{}, Reset: reset}
	for _, id := range slices.Sorted(maps.Keys(pending)) {
		task, err := service.Get(ctx, view.owner, id)
		var detail *domain.Error
		if errors.As(err, &detail) && detail.Code == "NOT_FOUND" {
			snapshot.Changed, snapshot.Reset = []tasks.Task{}, true
			break
		}
		if err != nil {
			return false
		}
		taskErrorID(&task, view.id)
		snapshot.Changed = append(snapshot.Changed, task)
	}
	frame, err := snapshot.encode()
	if err != nil || !c.authorized() {
		return false
	}
	c.server.control.mu.Lock()
	defer c.server.control.mu.Unlock()
	if c.taskView == view {
		c.send(json.RawMessage(frame))
	}
	return true
}

func (s taskSnapshot) encode() (json.RawMessage, error) {
	for {
		frame, err := json.Marshal(controlEvent{Type: "event", Event: "tasks.snapshot", Data: s})
		if err != nil || len(frame) <= 1<<20 {
			return frame, err
		}
		// 超长显示名可能撑大首屏，保留真实游标，剩余记录仍可分页读取。
		s.Reset = true
		if len(s.Changed) > 0 {
			s.Changed = []tasks.Task{}
		} else if len(s.Page.Items) > 1 {
			s.Page.Items = s.Page.Items[:len(s.Page.Items)/2]
			s.Page.NextCursor = &s.Page.Items[len(s.Page.Items)-1].ID
		} else {
			return nil, errors.New("task exceeds control frame limit")
		}
	}
}

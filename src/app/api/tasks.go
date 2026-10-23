package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

type tasksAPI interface {
	List(context.Context, string, string, int) (tasks.Page, error)
	Get(context.Context, string, string) (tasks.Task, error)
	Cancel(context.Context, string, string) (tasks.Task, error)
}

func (s *Server) RegisterTasks(service tasksAPI) {
	s.Handle("GET /api/v1/tasks", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		owner, err := taskScope(r, session, true)
		if err != nil {
			return err
		}
		limit := 50
		if values, exists := r.URL.Query()["limit"]; exists {
			value := values[0]
			limit, err = strconv.Atoi(value)
			if err != nil || limit < 1 || limit > 200 {
				return domain.Fail("INVALID_ARGUMENT", "Task limit must be between 1 and 200")
			}
		}
		page, err := service.List(r.Context(), owner, r.URL.Query().Get("cursor"), limit)
		if err != nil {
			return err
		}
		for i := range page.Items {
			taskErrorID(&page.Items[i], r)
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/tasks/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		owner, err := taskScope(r, session, false)
		if err != nil {
			return err
		}
		task, err := service.Get(r.Context(), owner, r.PathValue("id"))
		if err != nil {
			return err
		}
		taskErrorID(&task, r)
		return WriteJSON(w, http.StatusOK, task)
	})
	s.Handle("POST /api/v1/tasks/{id}/cancel", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		owner, err := taskScope(r, session, false)
		if err != nil {
			return err
		}
		if err = DecodeJSON(w, r, &struct{}{}); err != nil {
			return err
		}
		task, err := service.Cancel(r.Context(), owner, r.PathValue("id"))
		if err != nil {
			return err
		}
		taskErrorID(&task, r)
		return WriteJSON(w, http.StatusOK, task)
	})
}

func taskScope(r *http.Request, session auth.Session, list bool) (string, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid task query encoding")
	}
	for key, values := range query {
		if len(values) != 1 || key != "scope" && (!list || key != "cursor" && key != "limit") {
			return "", domain.Fail("INVALID_ARGUMENT", "Invalid or repeated task parameter")
		}
	}
	if len(query.Get("cursor")) > 256 {
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid task cursor")
	}
	switch query.Get("scope") {
	case "", "mine":
		return session.User.ID, nil
	case "all":
		if session.User.Role != "admin" {
			return "", domain.Fail("ACCESS_DENIED", "Administrator role is required")
		}
		return "", nil
	default:
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid task scope")
	}
}

func taskErrorID(task *tasks.Task, r *http.Request) {
	if task.Error != nil {
		detail := *task.Error
		detail.RequestID = RequestID(r)
		task.Error = &detail
	}
}

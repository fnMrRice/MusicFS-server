package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// RegisterWorks 注册通用作品资料端点，当前不接受文件路径或作品包导入。
func (s *Server) RegisterWorks(service *works.Service) {
	s.Handle("GET /api/v1/content-types", true, false, func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		if r.URL.RawQuery != "" {
			return invalidWorkInput()
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": service.ContentTypes()})
	})
	s.Handle("GET /api/v1/works", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := workQuery(r)
		if err != nil {
			return err
		}
		limit := query.Limit
		query.Limit++
		items, err := service.Works(r.Context(), session.User.ID, query)
		if err != nil {
			return err
		}
		var next *string
		if len(items) > limit {
			items = items[:limit]
			next = &items[len(items)-1].ID
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items, "nextAfter": next})
	})
	s.Handle("POST /api/v1/works", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ContentType *string `json:"contentType"`
			workFieldsInput
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ContentType == nil || !input.valid() {
			return invalidWorkInput()
		}
		result, err := service.CreateWork(r.Context(), session.User.ID, works.CreateRequest{
			ContentType: *input.ContentType, Title: *input.Title, Description: *input.Description,
			ExternalIDs: *input.ExternalIDs, Credits: *input.Credits, Tags: *input.Tags,
		}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, result)
	})
	s.Handle("GET /api/v1/works/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if r.URL.RawQuery != "" {
			return invalidWorkInput()
		}
		result, err := service.Work(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("PUT /api/v1/works/{id}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
			workFieldsInput
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || !input.valid() {
			return invalidWorkInput()
		}
		result, err := service.UpdateWork(r.Context(), session.User.ID, r.PathValue("id"), works.UpdateRequest{
			ExpectedRevision: *input.ExpectedRevision, Title: *input.Title, Description: *input.Description,
			ExternalIDs: *input.ExternalIDs, Credits: *input.Credits, Tags: *input.Tags,
		}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}

type workFieldsInput struct {
	Title       *string             `json:"title"`
	Description *string             `json:"description"`
	ExternalIDs *[]works.ExternalID `json:"externalIds"`
	Credits     *[]works.Credit     `json:"credits"`
	Tags        *[]string           `json:"tags"`
}

func (input workFieldsInput) valid() bool {
	return input.Title != nil && input.Description != nil && input.ExternalIDs != nil && input.Credits != nil && input.Tags != nil
}

func workQuery(r *http.Request) (works.Query, error) {
	query := works.Query{Limit: 50}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return query, invalidWorkInput()
	}
	for key, list := range values {
		if len(list) != 1 || list[0] == "" {
			return query, invalidWorkInput()
		}
		switch key {
		case "contentType":
			query.ContentType = list[0]
		case "packageKind":
			query.PackageKind = list[0]
		case "q":
			query.Q = list[0]
		case "tag":
			query.Tag = list[0]
		case "credit":
			query.Credit = list[0]
		case "after":
			query.After = list[0]
		case "limit":
			query.Limit, err = strconv.Atoi(list[0])
			if err != nil || query.Limit < 1 || query.Limit > works.MaxPageSize {
				return query, invalidWorkInput()
			}
		default:
			return query, invalidWorkInput()
		}
	}
	return works.NormalizeQuery(query)
}

func invalidWorkInput() error {
	return domain.Fail("INVALID_ARGUMENT", "Invalid work fields or query parameters")
}

package api

import (
	"encoding/json"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

func (s *Server) RegisterLibraryTasks(queue *tasks.OperationQueue) {
	send := func(w http.ResponseWriter, r *http.Request, owner, kind string, input any) error {
		key := r.Header.Get("Idempotency-Key")
		if len(key) < 8 || len(key) > 128 || r.URL.RawQuery != "" {
			return domain.Fail("INVALID_ARGUMENT", "Library tasks require an idempotency key and no query parameters")
		}
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		task, err := queue.Enqueue(r.Context(), owner, kind, key, payload)
		if err != nil {
			return err
		}
		taskErrorID(&task, RequestID(r))
		return WriteJSON(w, http.StatusAccepted, task)
	}
	s.Handle("POST /api/v1/tasks/album-scrape", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		// 空对象表示全库。拒绝无效筛选，防止意外扩大处理范围。
		var fields map[string]json.RawMessage
		if err := DecodeJSON(w, r, &fields); err != nil {
			return err
		}
		var input tasks.AlbumScrapeRequest
		for key, raw := range fields {
			var value *string
			if json.Unmarshal(raw, &value) != nil || value == nil {
				return domain.Fail("INVALID_ARGUMENT", "Invalid album filter")
			}
			switch key {
			case "providerInstanceId":
				if !instanceid.Valid(*value) {
					return domain.Fail("INVALID_ARGUMENT", "Invalid provider instance identifier")
				}
				input.ProviderInstanceID = *value
			case "q":
				input.Q = *value
			default:
				return domain.Fail("INVALID_ARGUMENT", "Unknown album filter")
			}
		}
		if err := (music.Query{Q: input.Q, ProviderID: input.ProviderInstanceID, Limit: 1}).Validate(); err != nil {
			return err
		}
		return send(w, r, session.User.ID, tasks.AlbumAutoScrape, input)
	})
	s.Handle("POST /api/v1/tasks/bulk-merge", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		input, err := readBulkMergeRequest(w, r)
		if err != nil {
			return err
		}
		return send(w, r, session.User.ID, tasks.BulkMerge, input)
	})
}

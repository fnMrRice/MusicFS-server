package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func (s *Server) registerOnlineAccounts(service *onlinehost.Service) {
	s.Handle("GET /api/v1/online-instances/{id}/account/options", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		result, err := service.AccountOptions(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, 200, result)
	})
	s.Handle("GET /api/v1/online-instances/{id}/account", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		result, err := service.Account(r.Context(), session.User.ID, r.PathValue("id"), online.AccountRequest{Action: "state"})
		if err != nil {
			return err
		}
		return WriteJSON(w, 200, result)
	})
	s.Handle("POST /api/v1/online-instances/{id}/account", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Action    string `json:"action"`
			LoginID   string `json:"loginId"`
			UserToken string `json:"userToken"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		switch input.Action {
		case "begin", "poll", "complete", "cancel", "check", "logout":
		default:
			return domain.Fail("INVALID_ARGUMENT", "Unknown platform account operation")
		}
		result, err := service.Account(r.Context(), session.User.ID, r.PathValue("id"), online.AccountRequest{Action: input.Action, LoginID: input.LoginID, UserToken: input.UserToken})
		if err != nil {
			return err
		}
		return WriteJSON(w, 200, result)
	})
}

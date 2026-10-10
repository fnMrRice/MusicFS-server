package api

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
)

func lifecyclePrincipal(s auth.Session) lifecycle.Principal {
	return lifecycle.Principal{UserID: s.User.ID, SessionID: s.ID, ExpiresAt: s.ExpiresAt}
}
func lifecycleLoopback(r *http.Request) error {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return domain.Fail("ACCESS_DENIED", "Lifecycle control requires a local connection")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return domain.Fail("ACCESS_DENIED", "Lifecycle control requires a local connection")
	}
	return nil
}
func (s *Server) RegisterLifecycle(manager *lifecycle.Manager) {
	s.Handle("GET /api/v1/lifecycle", true, false, func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		if err := lifecycleLoopback(r); err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, manager.Status())
	})
	// The manager owns its atomic ready/draining check. Release must remain
	// idempotent during draining, so these routes check CSRF explicitly.
	register := func(pattern string, h Handler) {
		s.Handle(pattern, true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			if err := lifecycleLoopback(r); err != nil {
				return err
			}
			if err := s.options.Auth.CheckMutation(session, r.Header.Get("Origin"), r.Header.Get("X-CSRF-Token")); err != nil {
				return err
			}
			return h(w, r, session)
		})
	}
	register("POST /api/v1/lifecycle/leases", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var in struct {
			InstanceID *string `json:"instanceId"`
			ClientID   *string `json:"clientId"`
		}
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		if in.InstanceID == nil || in.ClientID == nil {
			return domain.Fail("INVALID_ARGUMENT", "Lifecycle instance and client are required")
		}
		lease, err := manager.Acquire(lifecyclePrincipal(session), lifecycle.AcquireRequest{InstanceID: *in.InstanceID, ClientID: *in.ClientID}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, lease)
	})
	register("POST /api/v1/lifecycle/leases/{id}/renew", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var in struct {
			InstanceID *string `json:"instanceId"`
			Sequence   *int64  `json:"sequence"`
		}
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		if in.InstanceID == nil || in.Sequence == nil {
			return domain.Fail("INVALID_ARGUMENT", "Lifecycle instance and sequence are required")
		}
		lease, err := manager.Renew(lifecyclePrincipal(session), r.PathValue("id"), lifecycle.RenewRequest{InstanceID: *in.InstanceID, Sequence: *in.Sequence})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, lease)
	})
	register("POST /api/v1/lifecycle/leases/{id}/release", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var in struct {
			InstanceID *string `json:"instanceId"`
		}
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		if in.InstanceID == nil {
			return domain.Fail("INVALID_ARGUMENT", "Lifecycle instance is required")
		}
		lease, err := manager.Release(lifecyclePrincipal(session), r.PathValue("id"), lifecycle.ReleaseRequest{InstanceID: *in.InstanceID})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, lease)
	})
}

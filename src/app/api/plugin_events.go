package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func (s *Server) registerPluginEvents(manager *plugincontrol.Manager) {
	var mu sync.Mutex
	connections := map[string]int{}
	s.HandleStream("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		mu.Lock()
		if connections[session.ID] >= 8 {
			mu.Unlock()
			return domain.Fail("RATE_LIMITED", "Too many event streams")
		}
		connections[session.ID]++
		mu.Unlock()
		defer func() {
			mu.Lock()
			connections[session.ID]--
			if connections[session.ID] == 0 {
				delete(connections, session.ID)
			}
			mu.Unlock()
		}()
		updates, unsubscribe := manager.Subscribe()
		defer unsubscribe()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		controller := http.NewResponseController(w)
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		deadline := time.NewTimer(30 * time.Minute)
		defer deadline.Stop()
		for {
			var payload []byte
			select {
			case <-r.Context().Done():
				return nil
			case <-deadline.C:
				return nil
			case event := <-updates:
				body, _ := json.Marshal(event)
				payload = []byte(fmt.Sprintf("event: capabilities.changed\ndata: %s\n\n", body))
			case <-tick.C:
				cookie, err := r.Cookie(CookieName)
				if err != nil {
					return nil
				}
				current, err := s.options.Auth.Authenticate(r.Context(), cookie.Value)
				if err != nil || current.ID != session.ID {
					return nil
				}
				payload = []byte(": heartbeat\n\n")
			}
			if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil && err != http.ErrNotSupported {
				return nil
			}
			if _, err := w.Write(payload); err != nil {
				return nil
			}
			if err := controller.Flush(); err != nil {
				return nil
			}
		}
	})
}

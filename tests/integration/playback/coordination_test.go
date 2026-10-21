package playback_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func TestPlaybackRetainsSourceReadUntilSessionStops(t *testing.T) {
	for _, end := range []string{"stop", "revoke", "expire", "close"} {
		t.Run(end, func(t *testing.T) {
			service, repo, who, request, _ := playbackService(t)
			if end == "expire" {
				who.ID = "auth-expiring"
				who.ExpiresAt = time.Now().UTC().Add(500 * time.Millisecond)
				if err := repo.SaveAuth(context.Background(), who); err != nil {
					t.Fatal(err)
				}
			}
			d, err := service.Create(context.Background(), who, request, "retained-source")
			if err != nil {
				t.Fatal(err)
			}
			// Create has completed and revoked its preparation task. The immutable
			// cached representation must still retain its independent session read.
			if _, err = repo.library.TryExclusive(context.Background(), "admin", "root", domain.NewID()); err == nil {
				t.Fatal("cached playback did not defer writeback")
			}
			switch end {
			case "stop":
				if err = service.Stop(context.Background(), who, d.SessionID); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				service.Revoke(who.ID)
			case "close":
				service.Close()
			case "expire":
				deadline := time.After(3 * time.Second)
				for {
					lease, e := repo.library.TryExclusive(context.Background(), "admin", "root", domain.NewID())
					if e == nil {
						lease.Close()
						return
					}
					select {
					case <-deadline:
						t.Fatal("expired session retained read")
					case <-time.After(20 * time.Millisecond):
					}
				}
			}
			lease, err := repo.library.TryExclusive(context.Background(), "admin", "root", domain.NewID())
			if err != nil {
				t.Fatal("stopped session retained read", err)
			}
			lease.Close()
		})
	}
}

func TestFailedPlaybackDoesNotLeakSourceLease(t *testing.T) {
	service, repo, who, request, _ := playbackService(t)
	request.Capabilities.Formats = nil
	if _, err := service.Create(context.Background(), who, request, "failed-source"); err == nil {
		t.Fatal("unsupported playback succeeded")
	}
	lease, err := repo.library.TryExclusive(context.Background(), "admin", "root", domain.NewID())
	if err != nil {
		t.Fatal("failed create leaked read", err)
	}
	lease.Close()
}

package mediastore

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const DefaultGarbageGrace = 24 * time.Hour

type Collector struct {
	repo   GarbageRepository
	files  *Files
	grace  time.Duration
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewCollector(repo GarbageRepository, files *Files, grace time.Duration) (*Collector, error) {
	if grace == 0 {
		grace = DefaultGarbageGrace
	}
	if repo == nil || files == nil || grace < time.Hour {
		return nil, domain.Fail("INVALID_ARGUMENT", "Media garbage collection requires a grace period of at least one hour")
	}
	return &Collector{repo: repo, files: files, grace: grace}, nil
}

// Collect never enumerates storage directories. The repository durably claims
// only abandoned publication paths and rechecks all protections before removal.
func (c *Collector) Collect(ctx context.Context, now time.Time) (int, error) {
	if !c.mu.TryLock() {
		return 0, domain.Fail("CONFLICT", "Media garbage collection is running")
	}
	defer c.mu.Unlock()
	items, err := c.repo.ClaimMediaGarbage(ctx, now.Add(-c.grace), 128)
	if err != nil {
		return 0, err
	}
	started := time.Now()
	slog.DebugContext(ctx, "Media garbage collection batch claimed", "items", len(items))
	completed := 0
	var firstError error
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return completed, err
		}
		err = c.repo.DeleteMediaGarbage(ctx, item.ID, func(current Garbage) error {
			if current.ObjectID != item.ObjectID {
				return domain.Fail("CONFLICT", "Garbage publication changed")
			}
			return c.files.removeAbandoned(current.ObjectID)
		})
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		completed++
		if slog.Default().Enabled(ctx, slog.Level(-8)) {
			slog.Log(ctx, slog.Level(-8), "Abandoned media object removed", "completed", completed)
		}
	}
	if len(items) > 0 {
		slog.InfoContext(ctx, "Media garbage collection batch finished", "claimed", len(items), "completed", completed, "failed", len(items)-completed, "durationMs", time.Since(started).Milliseconds())
	}
	return completed, firstError
}

func (f *Files) removeAbandoned(id string) error {
	if !objectID.MatchString(id) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid abandoned object identifier")
	}
	for _, folder := range []string{"staging", "objects"} {
		path := folder + "/" + id
		info, err := f.root.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return domain.Fail("RESOURCE_CHANGED", "Abandoned media path is not a regular file")
		}
		if err = f.root.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err = syncDirectory(f.root, folder); err != nil {
			return err
		}
	}
	return nil
}

// Start is called once during bootstrap, after publication recovery. Work is
// bounded to one batch per hour; it never competes for playback worker slots.
func (c *Collector) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c.cancel, c.done = cancel, make(chan struct{})
	go func() {
		defer close(c.done)
		for {
			if _, err := c.Collect(ctx, time.Now()); err != nil && ctx.Err() == nil {
				slog.Warn("Media garbage collection failed", "errorCode", "TEMPORARY_FAILURE")
			}
			timer := time.NewTimer(time.Hour)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (c *Collector) Close() {
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
}

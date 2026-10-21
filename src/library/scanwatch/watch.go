package scanwatch

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

const MaxDirectories = 256

type Scanner interface {
	Scan(context.Context, string, string, library.ScanInput, string) (jobs.Job, bool, error)
	Job(context.Context, string, string) (jobs.Job, error)
}

type Options struct {
	Owner, InstanceID                string
	Plan                             pluginproto.ScanPlan
	Access                           *sourceaccess.Access
	Debounce, MaxDelay, FullInterval time.Duration
	Logger                           *slog.Logger
}

type Watcher struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func Start(ctx context.Context, scanner Scanner, options Options) (*Watcher, error) {
	if scanner == nil || options.Access == nil || options.Owner == "" || options.InstanceID == "" || options.Debounce <= 0 || options.MaxDelay < options.Debounce || options.FullInterval <= 0 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid watch configuration")
	}
	plan, err := options.Plan.Canonical()
	if err != nil {
		return nil, err
	}
	options.Plan = plan
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &Watcher{cancel: cancel, done: make(chan struct{})}
	go func() { defer close(w.done); w.run(ctx, scanner, options) }()
	return w, nil
}

func (w *Watcher) Close()                { w.once.Do(w.cancel); <-w.done }
func (w *Watcher) Done() <-chan struct{} { return w.done }

func (w *Watcher) run(ctx context.Context, scanner Scanner, o Options) {
	hints, failures := make(chan struct{}, 1), make(chan struct{}, 1)
	signal := func(ch chan struct{}) {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	var closeNative func()
	stopNative := func() {
		if closeNative != nil {
			closeNative()
			closeNative = nil
		}
	}
	defer stopNative()
	register := func() {
		stopNative()
		files, err := watchFiles(ctx, o)
		if err == nil {
			closeNative, err = startNative(ctx, files, func() { signal(hints) }, func() { signal(failures) })
		}
		for _, file := range files {
			file.Close()
		}
		if err != nil {
			o.Logger.Warn("scan watch degraded to periodic reconciliation", "instanceId", o.InstanceID, "errorCode", pluginproto.ErrorCode(err), "directoryLimit", MaxDirectories)
		} else {
			o.Logger.Info("scan watch active", "instanceId", o.InstanceID, "directories", len(files))
		}
	}
	register()
	p := pending{debounce: o.Debounce, maxDelay: o.MaxDelay}
	now := time.Now()
	p.reconcile(now)
	nextFull := now.Add(o.FullInterval)
	interval := min(200*time.Millisecond, o.Debounce/4, o.FullInterval/4)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	active := ""
	stateUnavailable := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-hints:
			p.hint(time.Now())
		case <-failures:
			stopNative()
			o.Logger.Warn("scan watch degraded to periodic reconciliation", "instanceId", o.InstanceID, "errorCode", "NOTIFICATION_UNAVAILABLE")
			p.reconcile(time.Now())
		case now = <-ticker.C:
			if !now.Before(nextFull) {
				p.reconcile(now)
				nextFull = now.Add(o.FullInterval)
			}
			if active != "" {
				job, err := scanner.Job(ctx, o.Owner, active)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					// Do not invent a replacement job when its state cannot be observed.
					if !stateUnavailable {
						o.Logger.Warn("scan watch waiting for job state", "instanceId", o.InstanceID, "jobId", active)
						stateUnavailable = true
					}
					continue
				}
				stateUnavailable = false
				if job.Status == "paused" {
					o.Logger.Warn("scan watch paused until restart", "instanceId", o.InstanceID, "jobId", active)
					return
				}
				if !jobs.IsTerminal(job.Status) {
					continue
				}
				active = ""
			}
			mode := p.take(now)
			if mode == "" {
				continue
			}
			// Register newly created directories before scanning their contents.
			// A racing creation during this handoff is still covered by periodic full scans.
			register()
			roots := make([]string, 0, len(o.Plan.Roots))
			for _, root := range o.Plan.Roots {
				roots = append(roots, root.RootID)
			}
			input := library.ScanInput{Mode: mode, SchemaRevision: o.Plan.SchemaRevision, RootIDs: roots}
			job, merged, err := scanner.Scan(ctx, o.Owner, o.InstanceID, input, domain.NewID())
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if pauseError(err) {
					o.Logger.Warn("scan watch paused until restart", "instanceId", o.InstanceID, "errorCode", pluginproto.ErrorCode(err))
					return
				}
				o.Logger.Warn("scan watch submission deferred to periodic reconciliation", "instanceId", o.InstanceID, "errorCode", pluginproto.ErrorCode(err))
				continue
			}
			active = job.ID
			if merged {
				p.hint(now)
			}
		}
	}
}

func pauseError(err error) bool {
	var failure *domain.Error
	if !errors.As(err, &failure) {
		return false
	}
	return failure.Code == "AUTH_REQUIRED" || failure.Code == "ACCESS_DENIED" || failure.Code == "SOURCE_OFFLINE"
}

func watchFiles(ctx context.Context, o Options) (files []*os.File, err error) {
	defer func() {
		if err != nil {
			for _, f := range files {
				f.Close()
			}
			files = nil
		}
	}()
	for _, root := range o.Plan.Roots {
		scope, scopeErr := o.Plan.Scope(root.RootID)
		if scopeErr != nil {
			return files, scopeErr
		}
		dirs, openErr := o.Access.WatchDirectories(ctx, scope, MaxDirectories-len(files))
		if openErr != nil {
			return files, openErr
		}
		for _, d := range dirs {
			files = append(files, d.File)
		}
	}
	return files, nil
}

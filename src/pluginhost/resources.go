package pluginhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

type entrySnapshot struct {
	binding          scanBinding
	rootID, identity string
	entries          []pluginproto.Entry
	issues           []string
	expires          time.Time
	deadline         time.Time
}

func (w *Worker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "host.entries":
		var r pluginproto.EntriesRequest
		if json.Unmarshal(raw, &r) != nil {
			return nil, pluginproto.Failure("ACCESS_DENIED", "invalid request")
		}
		return w.entries(ctx, r)
	case "host.state.get", "host.state.compareAndSet":
		var r pluginproto.StateRequest
		if json.Unmarshal(raw, &r) != nil {
			return nil, pluginproto.Failure("ACCESS_DENIED", "invalid request")
		}
		if r.Key == "root-identities" || strings.HasPrefix(r.Key, "host-rebind-") {
			return nil, pluginproto.Failure("ACCESS_DENIED", "protected state key")
		}
		if method == "host.state.get" {
			return w.state.load(r.Key)
		}
		return w.state.save(r)
	case "host.resource":
		if err := w.state.ready(); err != nil {
			return nil, err
		}
		var r pluginproto.Resource
		if json.Unmarshal(raw, &r) != nil {
			return nil, pluginproto.Failure("ACCESS_DENIED", "invalid request")
		}
		return w.Access.Register(sourceaccess.Scope{UserID: w.config.UserID, InstanceID: w.config.InstanceID, TaskID: "provider-resolve"}, r.RootID, r.RelativePath, r.ObservedVersion, 5*time.Minute)
	default:
		return nil, pluginproto.Failure("UNSUPPORTED", "unknown host method")
	}
}

func (w *Worker) entries(ctx context.Context, r pluginproto.EntriesRequest) (pluginproto.EntriesPage, error) {
	grant, err := w.scanAuthorization(r)
	if err != nil {
		return pluginproto.EntriesPage{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(grant.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := ctx.Err(); err != nil {
		return pluginproto.EntriesPage{}, err
	}
	if r.Limit < 1 || r.Limit > pluginproto.MaxPageItems {
		return pluginproto.EntriesPage{}, fmt.Errorf("ACCESS_DENIED: invalid page size")
	}
	var snapshot entrySnapshot
	var token string
	offset := 0
	if r.Cursor == "" {
		entries, identity, issues, err := w.Access.Entries(ctx, grant.binding.scope)
		if err != nil {
			return pluginproto.EntriesPage{}, err
		}
		var random [16]byte
		rand.Read(random[:])
		token = hex.EncodeToString(random[:])
		now := time.Now()
		idle, lifetime := w.config.snapshotTimeouts()
		snapshot = entrySnapshot{binding: grant.binding, rootID: r.RootID, identity: identity, entries: entries, issues: issues, expires: now.Add(idle), deadline: now.Add(lifetime)}
		if len(entries) > r.Limit {
			w.mu.Lock()
			if err := ctx.Err(); err != nil {
				w.mu.Unlock()
				return pluginproto.EntriesPage{}, err
			}
			for id, s := range w.snapshots {
				if !now.Before(s.expires) || !now.Before(s.deadline) {
					delete(w.snapshots, id)
				}
			}
			if len(w.snapshots) >= 16 {
				w.mu.Unlock()
				return pluginproto.EntriesPage{}, fmt.Errorf("RATE_LIMITED: too many scans")
			}
			w.snapshots[token] = snapshot
			w.mu.Unlock()
		}
	} else {
		var off string
		var ok bool
		token, off, ok = strings.Cut(r.Cursor, ":")
		if !ok {
			return pluginproto.EntriesPage{}, fmt.Errorf("ACCESS_DENIED: invalid scan cursor")
		}
		var err error
		offset, err = strconv.Atoi(off)
		if err != nil || offset < 0 {
			return pluginproto.EntriesPage{}, fmt.Errorf("ACCESS_DENIED: invalid scan cursor")
		}
		w.mu.Lock()
		snapshot, ok = w.snapshots[token]
		now := time.Now()
		if ok && (!now.Before(snapshot.expires) || !now.Before(snapshot.deadline)) {
			delete(w.snapshots, token)
			ok = false
		}
		if !ok || snapshot.rootID != r.RootID || !snapshot.binding.equal(grant.binding) || offset > len(snapshot.entries) {
			w.mu.Unlock()
			return pluginproto.EntriesPage{}, fmt.Errorf("RESOURCE_CHANGED: scan cursor expired")
		}
		if err := ctx.Err(); err != nil {
			w.mu.Unlock()
			return pluginproto.EntriesPage{}, err
		}
		idle, _ := w.config.snapshotTimeouts()
		snapshot.expires = now.Add(idle)
		if snapshot.expires.After(snapshot.deadline) {
			snapshot.expires = snapshot.deadline
		}
		w.snapshots[token] = snapshot
		w.mu.Unlock()
	}
	end := min(offset+r.Limit, len(snapshot.entries))
	page := pluginproto.EntriesPage{Items: snapshot.entries[offset:end], RootIdentity: snapshot.identity, Issues: snapshot.issues}
	if end < len(snapshot.entries) {
		page.NextCursor = token + ":" + strconv.Itoa(end)
	}
	return page, nil
}

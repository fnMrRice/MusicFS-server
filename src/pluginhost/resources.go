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
	rootID, identity string
	entries          []pluginproto.Entry
	issues           []string
	expires          time.Time
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
		if r.Key == "root-identities" {
			return nil, pluginproto.Failure("ACCESS_DENIED", "protected state key")
		}
		if method == "host.state.get" {
			return w.state.load(r.Key)
		}
		return w.state.save(r)
	case "host.resource":
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
	if r.Limit < 1 || r.Limit > pluginproto.MaxPageItems {
		return pluginproto.EntriesPage{}, fmt.Errorf("ACCESS_DENIED: invalid page size")
	}
	var snapshot entrySnapshot
	var token string
	offset := 0
	if r.Cursor == "" {
		entries, identity, issues, err := w.Access.Entries(ctx, r.RootID)
		if err != nil {
			return pluginproto.EntriesPage{}, err
		}
		counts := map[string]int{}
		for _, entry := range entries {
			counts[entry.FileIdentity]++
		}
		for i := range entries {
			if counts[entries[i].FileIdentity] > 1 {
				entries[i].FileIdentity += "/" + entries[i].RelativePath
			}
		}
		var random [16]byte
		rand.Read(random[:])
		token = hex.EncodeToString(random[:])
		snapshot = entrySnapshot{rootID: r.RootID, identity: identity, entries: entries, issues: issues, expires: time.Now().Add(5 * time.Minute)}
		w.mu.Lock()
		for id, s := range w.snapshots {
			if time.Now().After(s.expires) {
				delete(w.snapshots, id)
			}
		}
		if len(w.snapshots) >= 16 {
			w.mu.Unlock()
			return pluginproto.EntriesPage{}, fmt.Errorf("RATE_LIMITED: too many scans")
		}
		w.snapshots[token] = snapshot
		w.mu.Unlock()
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
		w.mu.Unlock()
		if !ok || snapshot.rootID != r.RootID || time.Now().After(snapshot.expires) || offset > len(snapshot.entries) {
			return pluginproto.EntriesPage{}, fmt.Errorf("RESOURCE_CHANGED: scan cursor expired")
		}
	}
	end := min(offset+r.Limit, len(snapshot.entries))
	page := pluginproto.EntriesPage{Items: snapshot.entries[offset:end], RootIdentity: snapshot.identity, Issues: snapshot.issues}
	if end < len(snapshot.entries) {
		page.NextCursor = token + ":" + strconv.Itoa(end)
	}
	return page, nil
}

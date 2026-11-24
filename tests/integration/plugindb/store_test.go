package plugindb_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestIsolationPersistenceAndConcurrentRevision(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plugins.sqlite")
	open := func(plugin, instance string) *state.Store {
		t.Helper()
		db, err := state.Open(ctx, path, plugin, instance)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	const first = "b1548e96-3081-5c9c-8376-bf290e369d51"
	const second = "16b003e5-1bab-52bb-be10-0e83ec1c40ea"
	a, b, sibling, other := open("plugin-a", first), open("plugin-a", first), open("plugin-a", second), open("plugin-b", first)
	r := pluginproto.StateRequest{Key: "preferences", Value: []byte(`{"recursive":true}`)}
	if _, err := a.CompareAndSet(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, db := range []*state.Store{sibling, other} {
		v, err := db.Get(ctx, r.Key)
		if err != nil || v.Revision != 0 || len(v.Value) != 0 {
			t.Fatalf("database scope leaked: %+v %v", v, err)
		}
	}
	r.Revision = 1
	var wins atomic.Int32
	var group sync.WaitGroup
	for _, db := range []*state.Store{a, b} {
		group.Add(1)
		go func(db *state.Store) {
			defer group.Done()
			if _, err := db.CompareAndSet(ctx, r); err == nil {
				wins.Add(1)
			}
		}(db)
	}
	group.Wait()
	if wins.Load() != 1 {
		t.Fatal("revision conflict was not atomic")
	}
	a.Close()
	b.Close()
	v, err := open("plugin-a", first).Get(ctx, r.Key)
	if err != nil || v.Revision != 2 || string(v.Value) != string(r.Value) {
		t.Fatalf("reopen lost settings: %+v %v", v, err)
	}
}

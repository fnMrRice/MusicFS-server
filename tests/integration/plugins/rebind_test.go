package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func rebindFixture(t *testing.T) (*pluginhost.Worker, pluginhost.Config, pluginhost.LocalRebind) {
	t.Helper()
	executable := buildWorker(t)
	raw, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	wave(t, filepath.Join(root, "中文 source.wav"))
	cfg := pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", UserID: "owner", Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}, StateDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	w, err := pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	page, err := w.Scan(ctx, scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "before", Generation: 1, Limit: 200}))
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("initial scan: %v", err)
	}
	old, err := w.Access.Inspect("music", page.Items[0].RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, old.RelativePath)
	raw, err = os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(file, file+".backup"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := w.Access.Inspect("music", old.RelativePath)
	if err != nil {
		t.Fatal(err)
	}
	r := pluginhost.LocalRebind{JobID: domain.NewID(), RootID: "music", RootIdentity: w.Access.Roots()[0].ExpectedIdentity, RelativePath: old.RelativePath, ExternalID: page.Items[0].ExternalID, OldIdentity: old.FileIdentity, OldVersion: old.ObservedVersion, NewIdentity: next.FileIdentity, NewVersion: next.ObservedVersion}
	return w, cfg, r
}

func TestLocalRebindPreservesIdentityAcrossWorkerRestart(t *testing.T) {
	w, cfg, r := rebindFixture(t)
	ctx := context.Background()
	observation, err := w.RebindLocalTrack(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if observation.ExternalID != r.ExternalID || observation.ObservedVersion != r.NewVersion || observation.ProviderSHA512 != nil {
		t.Fatal("rebind returned stale observation")
	}
	if _, err = w.RebindLocalTrack(ctx, r); err != nil {
		t.Fatal("idempotent rebind failed", err)
	}
	other := r
	other.OldVersion = "wrong"
	if _, err = w.RebindLocalTrack(ctx, other); err == nil {
		t.Fatal("same job accepted changed binding")
	}
	if _, err = w.Resolve(ctx, pluginproto.ResolveRequest{ExternalID: r.ExternalID, ExpectedVersion: r.OldVersion}); err == nil {
		t.Fatal("old version still resolves")
	}
	if _, err = w.Resolve(ctx, pluginproto.ResolveRequest{ExternalID: r.ExternalID, ExpectedVersion: r.NewVersion}); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	page, err := w.Scan(ctx, scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "after", Generation: 2, Limit: 200}))
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != r.ExternalID {
		t.Fatalf("replacement created a new source: %+v %v", page, err)
	}
	key := fmt.Sprintf("aliases-%x.json", sha256.Sum256([]byte(r.RootID+":"+r.OldIdentity)))
	raw, err := os.ReadFile(filepath.Join(cfg.StateDir, "plugin-state", key))
	if err != nil {
		t.Fatal(err)
	}
	var state pluginproto.StateValue
	if err = json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	var old struct {
		Paths map[string]string `json:"paths"`
	}
	if err = json.Unmarshal(state.Value, &old); err != nil {
		t.Fatal(err)
	}
	if old.Paths[r.RelativePath] != "" {
		t.Fatal("old inode still claimed the stable ID")
	}
}

func TestLocalRebindRefusesOccupiedNewIdentity(t *testing.T) {
	w, cfg, r := rebindFixture(t)
	key := fmt.Sprintf("aliases-%x.json", sha256.Sum256([]byte(r.RootID+":"+r.NewIdentity)))
	value, _ := json.Marshal(map[string]any{"paths": map[string]string{r.RelativePath: "ffffffffffffffffffffffffffffffff"}})
	raw, _ := json.Marshal(pluginproto.StateValue{Revision: 1, Value: value})
	if err := os.WriteFile(filepath.Join(cfg.StateDir, "plugin-state", key), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.RebindLocalTrack(context.Background(), r); err == nil {
		t.Fatal("foreign mapping overwritten")
	}
	got, err := os.ReadFile(filepath.Join(cfg.StateDir, "plugin-state", key))
	if err != nil || string(got) != string(raw) {
		t.Fatal("conflicting alias changed")
	}
}

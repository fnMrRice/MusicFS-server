package pluginhost_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/plugins/accountstore"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
)

func TestAccountMigrationAuthenticatesLegacyScopeAndRebindsUUID(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "netease-public")
	id := instanceid.Canonical("netease-public")
	dir := filepath.Join(stateDir, "credentials")
	legacy, err := accountstore.Open(dir, "netease\x00admin\x00netease-public")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"platform":"netease","userId":"admin","instanceId":"netease-public","accountId":"account","cookies":{"session":"test-secret"}}`)
	if err := legacy.Save(original); err != nil {
		t.Fatal(err)
	}
	current, err := accountstore.Open(dir, "netease\x00admin\x00"+id)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := onlineConfig(t)
	cfg.StateDir, cfg.InstanceID = stateDir, id
	w, err := pluginhost.StartOnline(t.Context(), cfg)
	if err != nil {
		t.Fatal("legacy account migration failed")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	secret, err := current.Load()
	if err != nil {
		t.Fatal(err)
	}
	var fields struct {
		InstanceID string            `json:"instanceId"`
		AccountID  string            `json:"accountId"`
		Cookies    map[string]string `json:"cookies"`
	}
	if json.Unmarshal(secret, &fields) != nil || fields.InstanceID != id || fields.AccountID != "account" || fields.Cookies["session"] != "test-secret" {
		t.Fatal("account identity or credential was not preserved")
	}
	if _, err := current.Load(); err != nil {
		t.Fatal("migrated credential does not authenticate its UUID")
	}
	if _, err := legacy.Load(); err == nil {
		t.Fatal("migrated credential still authenticates the old scope")
	}
	w, err = pluginhost.StartOnline(t.Context(), cfg)
	if err != nil {
		t.Fatal("migration is not restart-safe")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAccountMigrationRejectsDifferentInstanceAndOwner(t *testing.T) {
	for _, owner := range []string{"admin", "other"} {
		t.Run(owner, func(t *testing.T) {
			stateDir := filepath.Join(t.TempDir(), "old")
			dir := filepath.Join(stateDir, "credentials")
			legacy, err := accountstore.Open(dir, "netease\x00admin\x00old")
			if err != nil {
				t.Fatal(err)
			}
			if err := legacy.Save([]byte(`{"platform":"netease","userId":"admin","instanceId":"old","accountId":"account"}`)); err != nil {
				t.Fatal(err)
			}
			id := instanceid.Canonical("different")
			if owner == "other" {
				id = instanceid.Canonical("old")
			}
			cfg, _ := onlineConfig(t)
			cfg.StateDir, cfg.UserID, cfg.InstanceID = stateDir, owner, id
			if w, err := pluginhost.StartOnline(t.Context(), cfg); err == nil {
				w.Close()
				t.Fatal("account crossed the instance or owner boundary")
			}
			if _, err := legacy.Load(); err != nil {
				t.Fatal("rejected migration modified the original credential")
			}
		})
	}
}

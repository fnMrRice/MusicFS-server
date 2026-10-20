package pluginhost

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/accountstore"
	"github.com/fnMrRice/MusicFS-server/src/instanceid"
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
	w := OnlineWorker{config: OnlineConfig{StateDir: stateDir, Platform: "netease", UserID: "admin", InstanceID: id}}
	secret, err := w.loadAccount(current)
	if err != nil {
		t.Fatal("legacy account migration failed")
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
	if _, err := w.loadAccount(current); err != nil {
		t.Fatal("migration is not restart-safe")
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
			current, err := accountstore.Open(dir, "netease\x00"+owner+"\x00"+id)
			if err != nil {
				t.Fatal(err)
			}
			w := OnlineWorker{config: OnlineConfig{StateDir: stateDir, Platform: "netease", UserID: owner, InstanceID: id}}
			if _, err := w.loadAccount(current); err == nil {
				t.Fatal("account crossed the instance or owner boundary")
			}
			if _, err := legacy.Load(); err != nil {
				t.Fatal("rejected migration modified the original credential")
			}
		})
	}
}

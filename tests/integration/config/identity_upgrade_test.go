package config_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
)

func legacyLocalConfiguration(t *testing.T) (string, string, config.LocalSettings) {
	t.Helper()
	base := t.TempDir()
	path := filepath.Join(base, "server.yaml")
	raw := `configVersion: 1
pluginBundle: ''
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
plugins:
  - pluginId: musicfs.localfiles
    enabled: true
    executable: './local-worker'
    instances:
      - instanceId: local
        enabled: true
        config: {}
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(base, "data", "providers", "plugin-config.sqlite")
	if err := os.MkdirAll(filepath.Dir(database), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.ToSlash(database))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE plugin_values(scope TEXT NOT NULL,key TEXT NOT NULL,revision INTEGER NOT NULL,value BLOB NOT NULL,PRIMARY KEY(scope,key))`); err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	settings := config.LocalSettings{Name: "Saved library", MusicRoots: []config.MusicRoot{{ID: "music", Name: "Music", Path: music}}}
	for key, value := range map[string]any{
		"host-configuration":            settings,
		"host-default-read-directories": config.Permissions{ReadDirectories: []string{music}},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO plugin_values VALUES(?,?,7,?)`, "musicfs.plugin.source.localfiles\x00"+instanceid.Canonical("local"), key, encoded); err != nil {
			t.Fatal(err)
		}
	}
	return path, database, settings
}

func TestConfigurationReadLeavesLegacyPluginDatabaseUnchanged(t *testing.T) {
	path, database, settings := legacyLocalConfiguration(t)
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := config.ReadDocument(path, map[string]string{"ADMIN_TOKEN": "integration-configuration-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	p := doc.Config.Plugins[0]
	i := p.Instances[0]
	if p.PluginID != "musicfs.plugin.source.localfiles" || i.InstanceID != instanceid.Canonical("local") || i.Name != settings.Name || !reflect.DeepEqual(i.Config.MusicRoots, settings.MusicRoots) || i.PendingConfiguration {
		t.Fatalf("legacy settings or permissions were lost: %+v", i)
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("configuration read migrated state before taking the data lock: %v", err)
	}
}

func TestUnfinishedLegacyWritebackBlocksBeforePluginDatabaseMigration(t *testing.T) {
	for _, state := range []string{"recovery", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			assertLegacyWritebackBlocksUpgrade(t, state)
		})
	}
}

func assertLegacyWritebackBlocksUpgrade(t *testing.T, state string) {
	t.Helper()
	path, database, _ := legacyLocalConfiguration(t)
	c, err := config.Load(path, map[string]string{"ADMIN_TOKEN": "integration-configuration-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(context.Background(), filepath.Join(c.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.DB.Exec(`INSERT INTO tracks VALUES('track','admin','local','track','music','track.flac','v1','asset','audio/flac',12,'{"id":"track","providerInstanceId":"local"}')`)
	id := strings.Repeat("a", 48)
	if err == nil {
		_, err = store.DB.Exec(`INSERT INTO writeback_jobs(id,owner,source_id,state,old_size,work,created_at,updated_at) VALUES(?,'admin','track',?,12,'{"Target":{"instanceId":"local"}}','2026-10-20T00:00:00Z','2026-10-20T00:00:00Z')`, id, state)
	}
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	var journalPath string
	var journalBytes []byte
	if state == "succeeded" {
		// The database commit succeeded, but the process stopped before the
		// final journal save. The database state alone cannot detect this.
		journalPath = filepath.Join(c.DataRoot, "writeback", id+".json")
		if err := os.MkdirAll(filepath.Dir(journalPath), 0700); err != nil {
			t.Fatal(err)
		}
		journalBytes, err = json.Marshal(writeback.Record{ID: id, Owner: "admin", Target: writeback.Target{InstanceID: "local"}, Stage: "installed"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journalPath, journalBytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(database)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listen = listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = bootstrap.Serve(ctx, c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "finish writeback recovery with the previous server") {
		t.Fatalf("expected actionable recovery barrier: %v", err)
	}
	after, err := os.ReadFile(database)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("blocked upgrade modified the legacy plugin database: %v", err)
	}
	store, err = sqlite.Open(context.Background(), filepath.Join(c.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var provider string
	if err := store.DB.QueryRow(`SELECT provider FROM tracks WHERE id='track'`).Scan(&provider); err != nil || provider != "local" {
		t.Fatalf("blocked upgrade modified the main database namespace: %v", err)
	}
	if journalPath != "" {
		after, err := os.ReadFile(journalPath)
		if err != nil || !bytes.Equal(journalBytes, after) {
			t.Fatalf("blocked upgrade modified the recovery journal: %v", err)
		}
	}
}

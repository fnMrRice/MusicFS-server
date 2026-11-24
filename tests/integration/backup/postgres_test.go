package backup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestPostgresBackupRestoresDatabaseAndPrivateStateToSQLite(t *testing.T) {
	options := testkit.PostgresOptions(t)
	ctx := t.Context()
	root := t.TempDir()
	credential := []byte("fixture-password-for-trust-authentication")
	if file := options["passwordFile"]; file != "" {
		var err error
		credential, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
	}
	passwordDir := filepath.Join(root, "data", "providers")
	if err := os.MkdirAll(passwordDir, 0700); err != nil {
		t.Fatal(err)
	}
	options["passwordFile"] = filepath.Join(passwordDir, "postgres-password")
	if err := os.WriteFile(options["passwordFile"], credential, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(options["passwordFile"], filepath.Join(passwordDir, "linked-credential")); err != nil {
		t.Fatal(err)
	}
	storageConfig := config.StorageConfig{Backend: "postgres", Options: options}
	raw, err := yaml.Marshal(map[string]any{"configVersion": 1, "allowLoopbackHttp": true,
		"allowedOrigins": []string{"http://127.0.0.1:8080"}, "plugins": []any{}, "storage": storageConfig})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := config.ReadDocument(path, map[string]string{"ADMIN_TOKEN": "postgres-backup-test-only-token"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(ctx, "postgres", storage.Options{DataRoot: doc.Config.DataRoot, Values: options})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const plugin = "musicfs.plugin.test-backup"
	const instance = "fde39db7-04cf-4c25-a04c-75d763a0a6bc"
	if _, err := store.PutPluginSettings(ctx, "admin", plugin, 0, settings.Values{"album": "专辑"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// 模拟已删除的末尾编号，检查恢复后仍保留序列高水位。
	native := testkit.PostgresDatabase(t, options)
	if _, err := native.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('playlist_links','id'),42,true)`); err != nil {
		t.Fatal(err)
	}
	statePath := config.PluginDatabasePath(doc.Config)
	pgState, err := plugindb.OpenBackend(ctx, config.PluginDatabaseBackend(doc.Config), statePath, plugin, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer pgState.Close()
	const key = "progress"
	for i, value := range []string{`{"position":1}`, `{"position":42}`} {
		if _, err := pgState.CompareAndSet(ctx, pluginproto.StateRequest{Key: key, Revision: int64(i), Value: []byte(value)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := pgState.Close(); err != nil {
		t.Fatal(err)
	}
	// 旧安装残留的 SQLite 库和日志不能覆盖已选 PostgreSQL 的状态快照。
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.WriteFile(statePath+suffix, []byte("stale local state"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	bundle, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Verify(ctx, bundle.Directory); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"postgres-password", "linked-credential"} {
		if _, err := os.Stat(filepath.Join(bundle.Directory, "state", "providers", name)); !os.IsNotExist(err) {
			t.Fatalf("backup included database credential file %s", name)
		}
	}
	result, err := backup.Restore(ctx, bundle.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := config.Load(result.Configuration, map[string]string{"ADMIN_TOKEN": "restored-backup-test-only-token"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Storage.Backend != "sqlite" {
		t.Fatalf("restored backend = %s", restored.Storage.Backend)
	}
	local, err := sqlite.OpenMaintenance(ctx, filepath.Join(restored.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	var high int64
	if err := local.DB.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name='playlist_links'`).Scan(&high); err != nil || high != 42 {
		t.Fatalf("restored sequence = %d, err = %v", high, err)
	}
	record, err := local.GetPluginSettings(ctx, "admin", plugin)
	if err != nil || record.Revision != 1 || record.Values["album"] != "专辑" {
		t.Fatalf("restored settings = %+v, err = %v", record, err)
	}
	value, err := plugindb.ReadBackend(ctx, config.PluginDatabaseBackend(restored), config.PluginDatabasePath(restored), plugin, instance, key)
	if err != nil || value.Revision != 2 || string(value.Value) != `{"position":42}` {
		t.Fatalf("restored private state = %+v, err = %v", value, err)
	}
}

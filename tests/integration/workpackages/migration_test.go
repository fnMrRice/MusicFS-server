package workpackages_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestPackageKindMigrationUpgradesLegacyDocumentsAndImportReceipts(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "legacy.sqlite")
	uriPath := filepath.ToSlash(filename)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", uri.String()+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 27 {
			break
		}
		if _, err = db.ExecContext(t.Context(), step.SQL); err != nil {
			t.Fatalf("prepare migration %d: %v", step.Version, err)
		}
		if _, err = db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	work := works.Work{ID: "legacy-work", ContentType: "music", Title: "Legacy album", Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	workRaw, err := json.Marshal(work)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), "INSERT INTO works VALUES(?,?,?,?,?,?,?)", work.ID, "alice", "music", work.Title, "", 1, workRaw); err != nil {
		t.Fatal(err)
	}
	// 027 实际保存的结构是根包对象及同层私有绑定，节点也内含私有版本。
	legacyRaw := []byte(`{"packageId":"legacy-package","workId":"legacy-work","providerInstanceId":"local","rootId":"music","relativePath":"RJ123456","name":"RJ123456","revision":1,"importedAt":"2026-10-22T00:00:00Z","rootIdentity":"root-identity","configRevision":"config-v1","signature":"snapshot-v1","nodes":[{"nodeId":"legacy-root","parentId":"","path":".","name":"RJ123456","kind":"directory","order":0,"mime":"","sizeBytes":0,"sourceTrackId":"","assetId":"","attachmentKind":"","track":null,"version":"directory-v1","sourcePath":"RJ123456","externalId":""}]}`)
	if _, err = db.ExecContext(t.Context(), "INSERT INTO work_packages VALUES(?,?,?,?,?,?,?,?)", "legacy-package", "alice", work.ID, "local", "music", "RJ123456", 1, legacyRaw); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(`{"previewId":"legacy-preview"}`))
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	if _, err = db.ExecContext(t.Context(), "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", "alice", "works/legacy-work/packages/import", "legacy-import", hex.EncodeToString(digest[:]), legacyRaw, expires); err != nil {
		t.Fatal(err)
	}
	untouched := map[string][]byte{"works/another-work/packages/import": legacyRaw, "works/create": []byte(`{"workId":"legacy-work"}`), "unrelated/binary": {0xff, 0x00}}
	for route, raw := range untouched {
		if _, err = db.ExecContext(t.Context(), "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", "alice", route, "unrelated-key", "other-digest", raw, expires); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err = os.Stat(filename + ".before-028"); err != nil {
		t.Fatal("migration backup is missing", err)
	}
	pkg, err := store.WorkPackage(t.Context(), "alice", work.ID, "legacy-package")
	if err != nil || pkg.Kind != "directory" || pkg.RootIdentity != "root-identity" || pkg.ConfigRevision != "config-v1" || pkg.Signature != "snapshot-v1" || len(pkg.Nodes) != 1 || pkg.Nodes[0].Version != "directory-v1" || pkg.Nodes[0].SourcePath != "RJ123456" {
		t.Fatal("legacy package or private bindings were not preserved", err)
	}
	replay, found, err := store.ReplayWorkPackage(t.Context(), "alice", work.ID, "legacy-preview", "legacy-import")
	if err != nil || !found || !reflect.DeepEqual(replay, pkg) {
		t.Fatal("legacy import response was not upgraded", err)
	}
	items, err := store.Works(t.Context(), "alice", works.Query{PackageKind: "directory", Limit: 10})
	if err != nil || len(items) != 1 || items[0].ID != work.ID {
		t.Fatal("legacy package did not populate the kind index", err)
	}
	for route, expected := range untouched {
		var raw []byte
		if err = store.DB.QueryRowContext(t.Context(), "SELECT result FROM idempotency WHERE owner=? AND route=?", "alice", route).Scan(&raw); err != nil || !reflect.DeepEqual(raw, expected) {
			t.Fatal("migration changed an unrelated response", route, err)
		}
	}
	var kind string
	if err = store.DB.QueryRowContext(t.Context(), "SELECT kind FROM work_packages WHERE id=?", pkg.ID).Scan(&kind); err != nil || kind != "directory" {
		t.Fatal("legacy kind column is missing", err)
	}
	if _, err = store.DB.ExecContext(t.Context(), "UPDATE work_packages SET kind='invalid'"); err == nil {
		t.Fatal("kind check constraint accepted an invalid value")
	}
}

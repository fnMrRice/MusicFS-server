package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestWorkLibrariesMigrationClassifiesOnlyMusicWithCDCopies(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy-libraries.sqlite")
	db := openMigrationFixture(t, file)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 35 {
			break
		}
		if _, err = db.ExecContext(t.Context(), step.SQL); err != nil {
			t.Fatal(step.Version, err)
		}
		if _, err = db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	fixtures := []struct {
		id, oldType, newType string
		kinds                []string
		revision             int64
	}{
		{"disc", "music", "cd_album", []string{"cd_dump", "directory"}, 3},
		{"digital", "music", "music", []string{"directory"}, 2},
		{"voice", "asmr", "asmr", []string{"cd_dump"}, 2},
		{"empty", "music", "music", nil, 2},
	}
	for _, work := range fixtures {
		_, err := db.ExecContext(t.Context(), `INSERT INTO works(id,owner,content_type,title,description,revision,document)
			VALUES(?,'admin',?,?,'',2,json_object('workId',?,'contentType',?,'title',?,'revision',2,'tags',json_array('original')))`,
			work.id, work.oldType, work.id, work.id, work.oldType, work.id)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range work.kinds {
			id := work.id + "-" + kind
			_, err := db.ExecContext(t.Context(), `INSERT INTO work_packages(id,owner,work_id,provider_id,root_id,relative_path,revision,document,kind)
				VALUES(?,'admin',?,'local','music',?,1,json_object('packageId',?,'workId',?,'kind',?,'revision',1,'nodes',json_array()),?)`,
				id, work.id, id, id, work.id, kind, kind)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, fixture := range fixtures {
		work, err := store.Work(t.Context(), "admin", fixture.id)
		if err != nil || work.ContentType != fixture.newType || work.Revision != fixture.revision ||
			work.ShowInTrackList || len(work.Tags) != 1 || work.Tags[0] != "original" {
			t.Fatalf("%s migrated to unexpected work %+v: %v", fixture.id, work, err)
		}
		for _, kind := range fixture.kinds {
			id := fixture.id + "-" + kind
			p, err := store.WorkPackage(t.Context(), "admin", fixture.id, id)
			if err != nil || p.ID != id || p.Kind != kind || p.Revision != 1 {
				t.Fatal("lost original package", id, p, err)
			}
		}
	}
	discs, err := store.Works(t.Context(), "admin", works.Query{ContentType: "cd_album", Limit: 10})
	if err != nil || len(discs) != 1 || discs[0].ID != "disc" {
		t.Fatal("CD library migration index disagrees with work details", discs, err)
	}
}

package storage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAlbumServiceProtectsActualFilesAndRetainsChoiceAfterRestart(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	root := t.TempDir()
	files, err := mediastore.OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	media := mediastore.NewContent(s, s, files)
	service := albums.New(s, media, nil)
	a := albumCreate(t, s, "admin", "local", "中文发行", tracks[0].ID)
	metadata := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: tracks[0].ID, AssetID: tracks[0].AssetID, AssetVersion: tracks[0].Asset.Version}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}
	first, err := media.Upload(ctx, "admin", metadata, "image/png", domain.NewID(), bytes.NewReader(testkit.ScraperPNG()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := media.Upload(ctx, "admin", metadata, "image/jpeg", domain.NewID(), bytes.NewReader(testkit.ScraperJPEG()))
	if err != nil {
		t.Fatal(err)
	}
	r := albums.SelectCoverRequest{Target: albums.Target{Kind: "sourceAlbum", ID: a.ID}, ExpectedTargetRevision: 1, ObjectID: first.ID}
	choice, err := service.SelectCover(ctx, "admin", r, "actual-album-choice")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, "objects", second.ID)); err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.ObjectID = second.ID
	bad.ExpectedRevision = 1
	_, err = service.SelectCover(ctx, "admin", bad, "broken-album-choice")
	matchingError(t, err, "RESOURCE_CHANGED")
	retained, err := service.Cover(ctx, "admin", r.Target)
	if err != nil || !reflect.DeepEqual(retained, choice) {
		t.Fatal("missing new file replaced valid cover", err)
	}
	broken, err := s.ContentObject(ctx, "admin", second.ID)
	if err != nil || broken.State != "unavailable" {
		t.Fatal("broken object was not invalidated", err)
	}
	// A successful operation replay must not be changed by later storage loss.
	if err = os.Remove(filepath.Join(root, "objects", first.ID)); err != nil {
		t.Fatal(err)
	}
	replay, err := service.SelectCover(ctx, "admin", r, "actual-album-choice")
	if err != nil || !reflect.DeepEqual(replay, choice) {
		t.Fatal("replay revalidated historical IO", err)
	}
	_, err = service.SelectCover(ctx, "admin", albums.SelectCoverRequest{Target: r.Target, ExpectedTargetRevision: 1, ExpectedRevision: 1, ObjectID: first.ID}, domain.NewID())
	matchingError(t, err, "RESOURCE_CHANGED")
	current, err := service.Cover(ctx, "admin", r.Target)
	if err != nil || current.Binding.State != "unavailable" || !current.ManualLocked {
		t.Fatal("missing selected cover lost intent", err)
	}
	// Open the persisted DB through a second Store after the first connection is
	// quiescent; no module-local state may be required to retain the cover.
	var sequence int
	var database, file string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &database, &file); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Cover(ctx, "admin", r.Target)
	if err != nil || !reflect.DeepEqual(after, current) {
		t.Fatal("persisted cover changed after reopening", err)
	}
	if _, err = reopened.Cover(ctx, "other-owner", r.Target); err == nil {
		t.Fatal("reopened cover leaked owner scope")
	}
}

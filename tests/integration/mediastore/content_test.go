package mediastore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type fixture struct {
	store    *sqlite.Store
	files    *mediastore.Files
	service  *mediastore.ContentService
	metadata mediastore.ContentMetadata
	root     string
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_, _, err = s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("root"), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := library.Observation{Observation: pluginproto.Observation{ExternalID: "sample", ObservedVersion: "v1", RootID: "root", RelativePath: "audio.wav", SizeBytes: 128, MIME: "audio/wav", DisplayName: "Sample"}, Artists: []string{}}
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{o})); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, j, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListTracks(ctx, "admin", "local", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	track := page.Items[0]
	root := t.TempDir()
	files, err := mediastore.OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	return fixture{store: s, files: files, service: mediastore.NewContent(s, s, files, thumbnail.New()), root: root, metadata: mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}, Field: "lyrics", Language: "zh-Hans", Origin: mediastore.Origin{Kind: "upload"}}}
}

func upload(t *testing.T, f fixture, text string) mediastore.ContentObject {
	t.Helper()
	object, err := f.service.Upload(context.Background(), "admin", f.metadata, "text/plain", domain.NewID(), strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func TestUploadPreviewSelectionIsolationAndDamage(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	body := "[00:01.00]私有测试歌词\n[00:02.00]第二行"
	object := upload(t, f, body)
	selection, err := f.store.ContentSelection(ctx, "admin", f.metadata.Target.SourceTrackID)
	if err != nil || len(selection.Bindings) != 0 || selection.Revision != 0 {
		t.Fatalf("upload selected implicitly: %+v %v", selection, err)
	}
	_, file, err := f.service.Open(ctx, "admin", object.ID)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(actual, []byte(body)) {
		t.Fatal("published content differs")
	}
	if _, _, err = f.service.Open(ctx, "other", object.ID); err == nil {
		t.Fatal("private object exposed")
	}
	selection, err = f.service.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: f.metadata.Target, ObjectID: object.ID, ExpectedRevision: 0}, domain.NewID())
	if err != nil || len(selection.Bindings) != 1 || !selection.Bindings[0].ManualLocked {
		t.Fatalf("manual selection: %+v %v", selection, err)
	}
	var journal []byte
	if err = f.store.DB.QueryRow("SELECT document FROM media_publications WHERE object_id=?", object.ID).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(journal, []byte("私有测试歌词")) {
		t.Fatal("lyrics body entered database journal")
	}
	if err = os.Remove(filepath.Join(f.root, "objects", object.ID)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.service.Open(ctx, "admin", object.ID); err == nil {
		t.Fatal("missing object read succeeded")
	}
	selection, err = f.store.ContentSelection(ctx, "admin", f.metadata.Target.SourceTrackID)
	if err != nil || selection.Revision <= 1 || selection.Bindings[0].State != "unavailable" || !selection.Bindings[0].ManualLocked {
		t.Fatalf("missing selection not preserved: %+v %v", selection, err)
	}
}

func TestMissingPreviewCannotReplaceValidSelection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	previous := upload(t, f, "旧内容")
	key := domain.NewID()
	req := mediastore.SelectRequest{Target: f.metadata.Target, ObjectID: previous.ID, ExpectedRevision: 0}
	if _, err := f.service.SelectContent(ctx, "admin", req, key); err != nil {
		t.Fatal(err)
	}
	next := upload(t, f, "新内容")
	if err := os.Remove(filepath.Join(f.root, "objects", next.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: f.metadata.Target, ObjectID: next.ID, ExpectedRevision: 1}, domain.NewID()); err == nil {
		t.Fatal("missing preview replaced valid content")
	}
	current, err := f.store.ContentSelection(ctx, "admin", f.metadata.Target.SourceTrackID)
	if err != nil || current.Revision != 1 || len(current.Bindings) != 1 || current.Bindings[0].ObjectID != previous.ID {
		t.Fatalf("previous selection lost: %+v %v", current, err)
	}
	if err = os.Remove(filepath.Join(f.root, "objects", previous.ID)); err != nil {
		t.Fatal(err)
	}
	replayed, err := f.service.SelectContent(ctx, "admin", req, key)
	if err != nil || replayed.Revision != 1 || replayed.Bindings[0].ObjectID != previous.ID {
		t.Fatalf("original operation was not replayed: %+v %v", replayed, err)
	}
}

func TestUploadIdempotencyAndInvalidContentNeverPublishes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	key := domain.NewID()
	first, err := f.service.Upload(ctx, "admin", f.metadata, "text/plain", key, strings.NewReader("歌词"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Upload(ctx, "admin", f.metadata, "text/plain", key, strings.NewReader("歌词"))
	if err != nil || first.ID != second.ID {
		t.Fatal("upload replay differs")
	}
	if _, err = f.service.Upload(ctx, "admin", f.metadata, "text/plain", key, strings.NewReader("改动")); err == nil {
		t.Fatal("changed body reused key")
	}
	for _, test := range []struct{ body, mime string }{{"歌词", "image/png"}, {"\x00", "text/plain"}, {"", "text/plain"}, {strings.Repeat("x", (1<<20)+1), "text/plain"}} {
		if _, err = f.service.Upload(ctx, "admin", f.metadata, test.mime, domain.NewID(), strings.NewReader(test.body)); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = f.service.Upload(ctx, "admin", f.metadata, "text/plain", domain.NewID(), strings.NewReader("cancel")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	var count int
	if err = f.store.DB.QueryRow("SELECT COUNT(*) FROM media_publications").Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid input created publication: %d %v", count, err)
	}
}

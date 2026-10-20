package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func contentInput(track library.Track, language, body string) mediastore.UploadInput {
	sum := sha256.Sum256([]byte(body))
	return mediastore.UploadInput{Metadata: mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}, Field: "lyrics", Language: language, Format: "text", Origin: mediastore.Origin{Kind: "upload"}}, MIME: "text/plain; charset=utf-8", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
}

// Bodies never cross the storage interface. This helper supplies the verified
// file descriptor that the filesystem/service tests produce from actual bytes.
func contentPublish(t *testing.T, s *sqlite.Store, owner string, input mediastore.UploadInput) mediastore.ContentObject {
	t.Helper()
	ctx := context.Background()
	p, exists, err := s.PrepareContent(ctx, owner, input, domain.NewID())
	if err != nil || exists {
		t.Fatal("prepare failed", err)
	}
	o := p.Object
	o.Size, o.SHA256 = input.Size, input.SHA256
	if err = s.WrittenPublication(ctx, owner, p.ID, o); err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationStage(ctx, owner, p.ID, "written", "published"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReferencePublication(ctx, owner, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationStage(ctx, owner, p.ID, "referenced", "finished"); err != nil {
		t.Fatal(err)
	}
	object, err := s.ContentObject(ctx, owner, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func contentSelect(t *testing.T, s *sqlite.Store, object mediastore.ContentObject, revision int64, automatic bool) mediastore.Selection {
	t.Helper()
	result, err := s.SelectContent(context.Background(), "admin", mediastore.SelectRequest{Target: object.Metadata.Target, ObjectID: object.ID, ExpectedRevision: revision, Automatic: automatic}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

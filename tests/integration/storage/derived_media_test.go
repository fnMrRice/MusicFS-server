package storage_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func derivedArtwork(t *testing.T, width, height int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 200, G: 80, B: 20, A: 127})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDerivedThumbnailOwnershipPixelsBackupAndDamage(t *testing.T) {
	store, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	root := t.TempDir()
	files, err := mediastore.OpenFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	content := mediastore.NewContent(store, store, files)
	metadata := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: tracks[0].ID, AssetID: tracks[0].AssetID, AssetVersion: tracks[0].Asset.Version}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}
	original, err := content.Upload(ctx, "admin", metadata, "image/png", domain.NewID(), bytes.NewReader(derivedArtwork(t, 600, 300)))
	if err != nil {
		t.Fatal(err)
	}
	derived := content.Derived()
	thumb, err := derived.Thumbnail(ctx, "admin", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := derived.Thumbnail(ctx, "admin", original.ID)
	if err != nil || again.ID != thumb.ID {
		t.Fatalf("thumbnail reuse: %+v %v", again, err)
	}
	_, in, err := derived.Open(ctx, "admin", thumb.ID)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(in)
	in.Close()
	if err != nil || im.Bounds().Dx() != 256 || im.Bounds().Dy() != 128 {
		t.Fatal("thumbnail geometry", err)
	}
	pixel := color.NRGBAModel.Convert(im.At(20, 20)).(color.NRGBA)
	if pixel.A != 127 || pixel.R < 198 || pixel.R > 200 {
		t.Fatalf("alpha or color changed: %+v", pixel)
	}
	_, _, err = derived.Open(ctx, "bob", thumb.ID)
	matchingError(t, err, "NOT_FOUND")
	_, err = derived.Thumbnail(ctx, "bob", original.ID)
	matchingError(t, err, "NOT_FOUND")
	backup, err := store.CreateMediaBackup(ctx, domain.NewID(), filepath.Join(t.TempDir(), "snapshot.sqlite"))
	if err != nil || backup.ObjectCount != 2 {
		t.Fatalf("derived backup: %+v %v", backup, err)
	}
	if err = os.WriteFile(filepath.Join(root, "objects", thumb.ID), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = derived.Open(ctx, "admin", thumb.ID)
	matchingError(t, err, "RESOURCE_CHANGED")
	state, err := store.DerivedObject(ctx, "admin", thumb.ID)
	if err != nil || state.State != "unavailable" {
		t.Fatal("damage not persistent", err)
	}
	replacement, err := derived.Thumbnail(ctx, "admin", original.ID)
	if err != nil || replacement.ID == thumb.ID {
		t.Fatal("damaged thumbnail cannot be rebuilt", err)
	}
	backup, err = store.CreateMediaBackup(ctx, domain.NewID(), filepath.Join(t.TempDir(), "snapshot.sqlite"))
	if err != nil || backup.ObjectCount != 3 {
		t.Fatalf("historical derived backup: %+v %v", backup, err)
	}
	// Even an inconsistent journal must not make a historical reference garbage.
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err = store.DB.Exec("UPDATE media_publications SET status='aborted',updated_at=? WHERE object_id=?", old, thumb.ID); err != nil {
		t.Fatal(err)
	}
	garbage, err := store.ClaimMediaGarbage(ctx, time.Now().Add(-24*time.Hour), 128)
	if err != nil || len(garbage) != 0 {
		t.Fatal("derived reference was collected", err)
	}
	if _, err = store.DB.Exec("UPDATE tracks SET owner='bob' WHERE id=?", tracks[0].ID); err != nil {
		t.Fatal(err)
	}
	_, err = store.DerivedObject(ctx, "admin", replacement.ID)
	matchingError(t, err, "NOT_FOUND")
	_, _, err = derived.Open(ctx, "admin", replacement.ID)
	matchingError(t, err, "NOT_FOUND")
}

type derivedJournalFault struct {
	mediastore.Repository
	beforeReference bool
	finish          bool
}

func (f *derivedJournalFault) ReferencePublication(ctx context.Context, owner, id string) error {
	if f.beforeReference {
		f.beforeReference = false
		return errors.New("injected derived publication failure")
	}
	return f.Repository.ReferencePublication(ctx, owner, id)
}
func (f *derivedJournalFault) SetPublicationStage(ctx context.Context, owner, id, from, to string) error {
	if f.finish && to == "finished" {
		f.finish = false
		return errors.New("injected derived completion failure")
	}
	return f.Repository.SetPublicationStage(ctx, owner, id, from, to)
}

func TestDerivedPublicationRecoveryAndNoUpscale(t *testing.T) {
	for _, referenced := range []bool{false, true} {
		t.Run(map[bool]string{false: "published", true: "referenced"}[referenced], func(t *testing.T) {
			store, _, tracks := matchingFixture(t, 1)
			ctx := context.Background()
			files, err := mediastore.OpenFiles(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			plain := mediastore.NewContent(store, store, files)
			m := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: tracks[0].ID, AssetID: tracks[0].AssetID, AssetVersion: tracks[0].Asset.Version}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}
			original, err := plain.Upload(ctx, "admin", m, "image/png", domain.NewID(), bytes.NewReader(derivedArtwork(t, 13, 9)))
			if err != nil {
				t.Fatal(err)
			}
			fault := &derivedJournalFault{Repository: store, beforeReference: !referenced, finish: referenced}
			service := mediastore.NewContent(store, fault, files).Derived()
			if _, err = service.Thumbnail(ctx, "admin", original.ID); err == nil {
				t.Fatal("failure not injected")
			}
			journal, err := mediastore.New(store, files)
			if err != nil {
				t.Fatal(err)
			}
			if err = journal.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			thumb, err := plain.Derived().Thumbnail(ctx, "admin", original.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, in, err := plain.Derived().Open(ctx, "admin", thumb.ID)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(in)
			in.Close()
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(bytes.NewReader(body))
			if err != nil || cfg.Width != 13 || cfg.Height != 9 {
				t.Fatal("small image upscaled", err)
			}
		})
	}
}

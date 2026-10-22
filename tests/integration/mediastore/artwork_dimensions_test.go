package mediastore_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestArtworkDimensionsAreDerivedFromUploadedBytesAndPersisted(t *testing.T) {
	f := setup(t)
	metadata := f.metadata
	metadata.Field, metadata.Language = "artwork", ""
	for _, format := range []string{"png", "jpeg"} {
		picture := image.NewRGBA(image.Rect(0, 0, 37, 19))
		var body bytes.Buffer
		var err error
		if format == "png" {
			err = png.Encode(&body, picture)
		} else {
			err = jpeg.Encode(&body, picture, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		key := domain.NewID()
		object, err := f.service.Upload(t.Context(), "admin", metadata, "image/"+format, key, bytes.NewReader(body.Bytes()))
		if err != nil || object.Width != 37 || object.Height != 19 {
			t.Fatal("image dimensions were not derived from actual bytes", object, err)
		}
		stored, err := f.store.ContentObject(t.Context(), "admin", object.ID)
		if err != nil || stored.Width != object.Width || stored.Height != object.Height {
			t.Fatal("image dimensions were not persisted", stored, err)
		}
		replayed, err := f.service.Upload(t.Context(), "admin", metadata, "image/"+format, key, bytes.NewReader(body.Bytes()))
		if err != nil || replayed.ID != object.ID || replayed.Width != 37 || replayed.Height != 19 {
			t.Fatal("upload replay lost image dimensions", replayed, err)
		}
		var raw []byte
		if err = f.store.DB.QueryRowContext(t.Context(), "SELECT document FROM media_content_objects WHERE id=?", object.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if json.Unmarshal(raw, &document) != nil || document["width"] != float64(37) || document["height"] != float64(19) {
			t.Fatal("dimensions are missing from the media object document", string(raw))
		}
	}
	lyrics := upload(t, f, "Synthetic lyrics have no image dimensions")
	if lyrics.Width != 0 || lyrics.Height != 0 {
		t.Fatal("lyrics received image dimensions", lyrics)
	}
	raw, err := json.Marshal(lyrics)
	if err != nil || bytes.Contains(raw, []byte(`"width"`)) || bytes.Contains(raw, []byte(`"height"`)) {
		t.Fatal("unknown dimensions were not omitted", err)
	}
	var old mediastore.ContentObject
	if err = json.Unmarshal([]byte(`{"objectId":"old","metadata":{"field":"artwork"}}`), &old); err != nil || old.Width != 0 || old.Height != 0 {
		t.Fatal("missing dimensions were guessed", old, err)
	}
}

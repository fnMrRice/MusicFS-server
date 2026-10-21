package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttransform"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type observedThumbnail struct {
	contenttransform.Thumbnailer
	calls  int
	output []byte
}

func (p *observedThumbnail) Thumbnail(ctx context.Context, body []byte) ([]byte, error) {
	p.calls++
	if p.output != nil {
		return p.output, nil
	}
	return p.Thumbnailer.Thumbnail(ctx, body)
}

func TestThumbnailPluginRetainsHostOwnershipCacheAndValidation(t *testing.T) {
	store, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	plugin := &observedThumbnail{Thumbnailer: thumbnail.New(), output: []byte("invalid PNG")}
	content := mediastore.NewContent(store, store, files, plugin)
	m := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: tracks[0].ID, AssetID: tracks[0].AssetID, AssetVersion: tracks[0].Asset.Version}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}
	original, err := content.Upload(ctx, "admin", m, "image/png", domain.NewID(), bytes.NewReader(derivedArtwork(t, 13, 9)))
	if err != nil {
		t.Fatal(err)
	}
	derived := content.Derived()
	if _, err = derived.Thumbnail(ctx, "bob", original.ID); err == nil || plugin.calls != 0 {
		t.Fatal("unauthorized owner reached the transform plugin")
	}
	if _, err = derived.Thumbnail(ctx, "admin", original.ID); err == nil || plugin.calls != 1 {
		t.Fatal("invalid plugin output was published")
	}
	plugin.output = derivedArtwork(t, 257, 1)
	if _, err = derived.Thumbnail(ctx, "admin", original.ID); err == nil {
		t.Fatal("oversized plugin output was published")
	}
	plugin.output = nil
	first, err := derived.Thumbnail(ctx, "admin", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := derived.Thumbnail(ctx, "admin", original.ID)
	if err != nil || first.ID != second.ID || plugin.calls != 3 {
		t.Fatalf("host cache did not reuse the plugin result: %v, calls=%d", err, plugin.calls)
	}
}

func TestThumbnailRequiresExplicitPlugin(t *testing.T) {
	store, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	content := mediastore.NewContent(store, store, files, nil)
	m := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: tracks[0].ID, AssetID: tracks[0].AssetID, AssetVersion: tracks[0].Asset.Version}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}
	original, err := content.Upload(ctx, "admin", m, "image/png", domain.NewID(), bytes.NewReader(derivedArtwork(t, 13, 9)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = content.Derived().Thumbnail(ctx, "admin", original.ID)
	matchingError(t, err, "UNSUPPORTED")
}

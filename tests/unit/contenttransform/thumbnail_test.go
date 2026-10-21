package contenttransform_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
)

func TestThumbnailPluginJPEGAndCancellation(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 300, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 300; x++ {
			source.SetRGBA(x, y, color.RGBA{R: 100, G: 60, B: 20, A: 255})
		}
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, source, nil); err != nil {
		t.Fatal(err)
	}
	plugin := thumbnail.New()
	output, err := plugin.Thumbnail(context.Background(), input.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(output))
	if err != nil || decoded.Bounds().Dx() != 128 || decoded.Bounds().Dy() != 256 {
		t.Fatalf("unexpected JPEG thumbnail: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = plugin.Thumbnail(ctx, input.Bytes()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was ignored: %v", err)
	}
	if _, err = plugin.Thumbnail(context.Background(), []byte("invalid")); err == nil {
		t.Fatal("invalid image was accepted")
	}
}

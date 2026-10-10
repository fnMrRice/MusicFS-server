package mediastore

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore/content"
)

func (s *DerivedService) Thumbnail(ctx context.Context, owner, id string) (DerivedObject, error) {
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	default:
		return DerivedObject{}, domain.Fail("RATE_LIMITED", "Thumbnail generation is busy")
	}
	parent, in, err := s.content.Open(ctx, owner, id)
	if err != nil {
		return DerivedObject{}, err
	}
	defer in.Close()
	if parent.Metadata.Field != "artwork" {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Only artwork has a thumbnail")
	}
	m := DerivedMetadata{Transform: "thumbnail256/v1", ParentObjectID: id, ParentSHA256: parent.SHA256}
	key, err := m.Key()
	if err != nil {
		return DerivedObject{}, err
	}
	if o, exists, e := s.existing(ctx, owner, key); e != nil || exists {
		return o, e
	}
	body, err := io.ReadAll(io.LimitReader(in, content.MaxArtworkBytes+1))
	if err != nil {
		return DerivedObject{}, err
	}
	if len(body) == 0 || len(body) > content.MaxArtworkBytes {
		return DerivedObject{}, domain.Fail("RESOURCE_CHANGED", "Invalid artwork size")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > content.MaxImagePixels {
		return DerivedObject{}, domain.Fail("UNSUPPORTED", "Invalid artwork dimensions")
	}
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return DerivedObject{}, domain.Fail("RESOURCE_CHANGED", "Artwork decode failed")
	}
	width, height := cfg.Width, cfg.Height
	if width > 256 || height > 256 {
		if width >= height {
			height = max(1, height*256/width)
			width = 256
		} else {
			width = max(1, width*256/height)
			height = 256
		}
	}
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	// Area averaging bounds processing by the verified input pixel count and
	// retains alpha. Small images are copied without upscaling.
	for y := 0; y < height; y++ {
		if err = ctx.Err(); err != nil {
			return DerivedObject{}, err
		}
		for x := 0; x < width; x++ {
			x0, x1 := x*cfg.Width/width, (x+1)*cfg.Width/width
			y0, y1 := y*cfg.Height/height, (y+1)*cfg.Height/height
			var red, green, blue, alpha, count uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := source.At(sx+source.Bounds().Min.X, sy+source.Bounds().Min.Y).RGBA()
					red += uint64(r)
					green += uint64(g)
					blue += uint64(b)
					alpha += uint64(a)
					count++
				}
			}
			var c color.NRGBA
			if alpha > 0 {
				c.R = uint8(red * 255 / alpha)
				c.G = uint8(green * 255 / alpha)
				c.B = uint8(blue * 255 / alpha)
				c.A = uint8(alpha / count >> 8)
			}
			result.SetNRGBA(x, y, c)
		}
	}
	var output bytes.Buffer
	if err = png.Encode(&output, result); err != nil {
		return DerivedObject{}, err
	}
	return s.publish(ctx, owner, "thumbnail", "image/png", m, output.Bytes())
}

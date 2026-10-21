// Package thumbnail 提供主进程内 PNG/JPEG 缩略图变换。
package thumbnail

import (
	"bytes"
	"context"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttransform"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type Plugin struct{}

var _ contenttransform.Thumbnailer = (*Plugin)(nil)

func New() *Plugin                { return &Plugin{} }
func (*Plugin) Transform() string { return contenttransform.Thumbnail256V1 }

func (*Plugin) Thumbnail(ctx context.Context, body []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(body) == 0 || len(body) > contenttransform.MaxArtworkBytes {
		return nil, fault.Fail("RESOURCE_CHANGED", "Invalid artwork size")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > contenttransform.MaxImagePixels {
		return nil, fault.Fail("UNSUPPORTED", "Invalid artwork dimensions")
	}
	source, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fault.Fail("RESOURCE_CHANGED", "Artwork decode failed")
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
	// 区域平均的处理量受已验证像素数约束，保留透明度且不放大小图。
	for y := 0; y < height; y++ {
		if err = ctx.Err(); err != nil {
			return nil, err
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
		return nil, err
	}
	return output.Bytes(), nil
}

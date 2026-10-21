package flac

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"unicode/utf8"
)

func encodePicture(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > 8<<20 {
		return nil, invalid("Artwork exceeds the supported limit")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16<<20 {
		return nil, unsupported("Artwork must be a bounded PNG or JPEG")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, unsupported("Artwork is incomplete")
	}
	var out bytes.Buffer
	put := func(n uint32) { _ = binary.Write(&out, binary.BigEndian, n) }
	mime := []byte("image/" + format)
	put(3)
	put(uint32(len(mime)))
	out.Write(mime)
	put(0) // 描述留空，不接受外部图片 URL。
	put(uint32(cfg.Width))
	put(uint32(cfg.Height))
	depth := uint32(24)
	if format == "png" && len(data) >= 26 {
		channels := map[byte]uint32{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}
		depth = uint32(data[24]) * channels[data[25]]
	}
	put(depth)
	colors := uint32(0)
	if palette, ok := decoded.ColorModel().(color.Palette); ok {
		colors = uint32(len(palette))
	}
	put(colors)
	put(uint32(len(data)))
	out.Write(data)
	return out.Bytes(), nil
}

func validatePicture(data []byte) error {
	if len(data) < 32 {
		return unsupported("Invalid FLAC picture")
	}
	data = data[4:]
	for i := range 2 {
		if len(data) < 4 {
			return unsupported("Invalid FLAC picture length")
		}
		n := uint64(binary.BigEndian.Uint32(data))
		data = data[4:]
		if n > uint64(len(data)) || (i == 1 && !utf8.Valid(data[:int(n)])) {
			return unsupported("Invalid FLAC picture content")
		}
		data = data[int(n):]
	}
	if len(data) < 20 || uint64(binary.BigEndian.Uint32(data[16:20])) != uint64(len(data)-20) {
		return unsupported("Invalid FLAC picture payload")
	}
	return nil
}

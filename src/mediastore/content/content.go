// Package content validates bounded lyrics and artwork independently of storage.
package content

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"unicode"
)

const MaxLyricsBytes = 1 << 20
const MaxArtworkBytes = 8 << 20
const MaxImagePixels = 16_000_000

// Validated is an in-memory body, never a database or control-protocol DTO.
type Validated struct {
	Bytes                []byte
	Format, MIME, SHA256 string
	Width, Height        int
}

var lrcTimestamp = regexp.MustCompile(`\[[0-9]{1,3}:[0-5][0-9](?:[.:][0-9]{1,3})?\]`)

func Validate(kind string, input []byte) (Validated, error) {
	var result Validated
	var err error
	switch kind {
	case "lyrics":
		if len(input) == 0 || len(input) > MaxLyricsBytes {
			return result, fmt.Errorf("UNSUPPORTED: lyrics size is outside limits")
		}
		result.Bytes, err = DecodeText(input)
		if err != nil {
			return result, err
		}
		if len(result.Bytes) == 0 || len(result.Bytes) > MaxLyricsBytes || len(bytes.TrimSpace(result.Bytes)) == 0 {
			return Validated{}, fmt.Errorf("UNSUPPORTED: lyrics are empty or exceed limits")
		}
		for _, r := range string(result.Bytes) {
			if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
				return Validated{}, fmt.Errorf("UNSUPPORTED: lyrics contain control characters")
			}
		}
		result.Format, result.MIME = "text", "text/plain; charset=utf-8"
		if lrcTimestamp.Match(result.Bytes) {
			result.Format = "lrc"
		}
	case "artwork":
		if len(input) == 0 || len(input) > MaxArtworkBytes {
			return result, fmt.Errorf("UNSUPPORTED: artwork size is outside limits")
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(input))
		if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
			return result, fmt.Errorf("UNSUPPORTED: invalid or oversized artwork")
		}
		if _, decodedFormat, err := image.Decode(bytes.NewReader(input)); err != nil || decodedFormat != format {
			return result, fmt.Errorf("UNSUPPORTED: damaged artwork")
		}
		result.Bytes = append([]byte(nil), input...)
		result.Format, result.MIME, result.Width, result.Height = format, "image/"+format, cfg.Width, cfg.Height
	default:
		return result, fmt.Errorf("INVALID_ARGUMENT: unsupported media content kind")
	}
	result.SHA256 = fmt.Sprintf("%x", sha256.Sum256(result.Bytes))
	return result, nil
}

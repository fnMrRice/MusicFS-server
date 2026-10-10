package scraperlocal

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/mediastore/content"
)

func terminated(b []byte, encoding byte) ([]byte, []byte, error) {
	step := 1
	if encoding == 1 || encoding == 2 {
		step = 2
	}
	for i := 0; i+step <= len(b); i += step {
		if b[i] == 0 && (step == 1 || b[i+1] == 0) {
			return b[:i], b[i+step:], nil
		}
	}
	return nil, nil, fmt.Errorf("UNSUPPORTED: unterminated ID3 text")
}

func id3Text(b []byte, encoding byte, description []byte) ([]byte, error) {
	if len(b) > content.MaxLyricsBytes {
		return nil, fmt.Errorf("UNSUPPORTED: lyrics frame exceeds limit")
	}
	switch encoding {
	case 0:
		b = bytes.TrimRight(b, "\x00")
		out := make([]byte, 0, len(b))
		for _, value := range b {
			out = utf8.AppendRune(out, rune(value))
		}
		return out, nil
	case 3:
		b = bytes.TrimRight(b, "\x00")
		if !utf8.Valid(b) {
			return nil, fmt.Errorf("UNSUPPORTED: invalid ID3 UTF-8")
		}
		return append([]byte(nil), b...), nil
	case 1, 2:
		for len(b) >= 2 && len(b)%2 == 0 && b[len(b)-1] == 0 && b[len(b)-2] == 0 {
			b = b[:len(b)-2]
		}
		var order binary.ByteOrder = binary.BigEndian
		if encoding == 1 {
			bom := b
			if len(bom) < 2 || !(bytes.HasPrefix(bom, []byte{0xff, 0xfe}) || bytes.HasPrefix(bom, []byte{0xfe, 0xff})) {
				bom = description
			} else {
				b = b[2:]
			}
			if bytes.HasPrefix(bom, []byte{0xff, 0xfe}) {
				order = binary.LittleEndian
			} else if !bytes.HasPrefix(bom, []byte{0xfe, 0xff}) {
				return nil, fmt.Errorf("UNSUPPORTED: ID3 UTF-16 byte order unavailable")
			}
		}
		return content.DecodeUTF16(b, order)
	default:
		return nil, fmt.Errorf("UNSUPPORTED: unknown ID3 encoding")
	}
}

func id3Lyrics(b []byte, version byte) ([]byte, string, error) {
	if len(b) < 5 || b[0] > 3 || (version == 3 && b[0] > 1) {
		return nil, "und", fmt.Errorf("UNSUPPORTED: invalid lyrics frame")
	}
	language := string(b[1:4])
	for _, char := range language {
		if char < 'a' || char > 'z' {
			return nil, "und", fmt.Errorf("UNSUPPORTED: invalid lyrics language")
		}
	}
	description, body, err := terminated(b[4:], b[0])
	if err != nil {
		return nil, language, err
	}
	text, err := id3Text(body, b[0], description)
	return text, language, err
}

func id3Picture(b []byte, version byte) ([]byte, error) {
	if len(b) < 4 || b[0] > 3 || (version == 3 && b[0] > 1) {
		return nil, fmt.Errorf("UNSUPPORTED: invalid picture frame")
	}
	mime, remaining, err := terminated(b[1:], 0)
	if err != nil || len(remaining) < 2 || string(mime) == "-->" {
		return nil, fmt.Errorf("UNSUPPORTED: linked or invalid picture frame")
	}
	_, picture, err := terminated(remaining[1:], b[0])
	if err != nil {
		return nil, err
	}
	return picture, nil
}

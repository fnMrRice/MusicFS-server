package content

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// DecodeText accepts only UTF-8 or BOM-declared UTF-16; no encoding guessing.
func DecodeText(input []byte) ([]byte, error) {
	if bytes.HasPrefix(input, []byte{0xef, 0xbb, 0xbf}) {
		input = input[3:]
	}
	if bytes.HasPrefix(input, []byte{0xff, 0xfe}) {
		return DecodeUTF16(input[2:], binary.LittleEndian)
	}
	if bytes.HasPrefix(input, []byte{0xfe, 0xff}) {
		return DecodeUTF16(input[2:], binary.BigEndian)
	}
	if !utf8.Valid(input) {
		return nil, fmt.Errorf("UNSUPPORTED: uncertain lyrics encoding")
	}
	return append([]byte(nil), input...), nil
}

func DecodeUTF16(input []byte, order binary.ByteOrder) ([]byte, error) {
	if len(input)%2 != 0 || order == nil {
		return nil, fmt.Errorf("UNSUPPORTED: invalid UTF-16 text")
	}
	result := make([]byte, 0, len(input))
	for i := 0; i < len(input); i += 2 {
		value := rune(order.Uint16(input[i:]))
		if value >= 0xd800 && value <= 0xdbff {
			if i+4 > len(input) {
				return nil, fmt.Errorf("UNSUPPORTED: truncated UTF-16 pair")
			}
			next := rune(order.Uint16(input[i+2:]))
			if next < 0xdc00 || next > 0xdfff {
				return nil, fmt.Errorf("UNSUPPORTED: invalid UTF-16 pair")
			}
			value = utf16.DecodeRune(value, next)
			i += 2
		} else if value >= 0xdc00 && value <= 0xdfff {
			return nil, fmt.Errorf("UNSUPPORTED: unpaired UTF-16 surrogate")
		}
		result = utf8.AppendRune(result, value)
		if len(result) > MaxLyricsBytes {
			return nil, fmt.Errorf("UNSUPPORTED: decoded text exceeds limit")
		}
	}
	return result, nil
}

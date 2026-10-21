package scraperlocal

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func (e *Engine) flac(ctx context.Context, r enrichment.Resource, reader io.ReaderAt) error {
	offset := int64(4)
	for index := 0; index < 1024; index++ {
		header, err := readBounded(ctx, reader, r.SizeBytes, offset, 4, 4)
		if err != nil {
			return err
		}
		kind := header[0] & 0x7f
		length := int64(header[1])<<16 | int64(header[2])<<8 | int64(header[3])
		offset += 4
		if kind == 127 || (index == 0 && (kind != 0 || length != 34)) || length > maxMetadataBytes-offset {
			return fmt.Errorf("UNSUPPORTED: invalid FLAC metadata layout")
		}
		body, err := readBounded(ctx, reader, r.SizeBytes, offset, length, maxMetadataBytes)
		if err != nil {
			return err
		}
		offset += length
		switch kind {
		case 4:
			if err = e.flacComments(r, body); err != nil {
				return err
			}
		case 6:
			picture, parseErr := flacPicture(body)
			if err = e.add(r, "artwork", "und", "embedded", "PICTURE", picture, parseErr); err != nil {
				return err
			}
		}
		if header[0]&0x80 != 0 {
			return nil
		}
	}
	return fmt.Errorf("RATE_LIMITED: too many FLAC metadata blocks")
}

func sized(b []byte, order binary.ByteOrder) ([]byte, []byte, error) {
	if len(b) < 4 {
		return nil, nil, fmt.Errorf("UNSUPPORTED: truncated metadata length")
	}
	length := uint64(order.Uint32(b))
	if length > uint64(len(b)-4) {
		return nil, nil, fmt.Errorf("UNSUPPORTED: invalid metadata length")
	}
	return b[4 : 4+int(length)], b[4+int(length):], nil
}

func (e *Engine) flacComments(r enrichment.Resource, b []byte) error {
	_, b, err := sized(b, binary.LittleEndian)
	if err != nil {
		return err
	}
	if len(b) < 4 {
		return fmt.Errorf("UNSUPPORTED: missing FLAC comment count")
	}
	count := binary.LittleEndian.Uint32(b)
	b = b[4:]
	if count > 4096 {
		return fmt.Errorf("RATE_LIMITED: too many FLAC comments")
	}
	for i := uint32(0); i < count; i++ {
		var value []byte
		value, b, err = sized(b, binary.LittleEndian)
		if err != nil {
			return err
		}
		key, text, ok := strings.Cut(string(value), "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(key)
		if key == "LYRICS" || key == "UNSYNCEDLYRICS" {
			var parseErr error
			if !utf8.ValidString(text) {
				parseErr = fmt.Errorf("UNSUPPORTED: invalid FLAC lyrics encoding")
			}
			if err = e.add(r, "lyrics", "und", "embedded", key, []byte(text), parseErr); err != nil {
				return err
			}
		}
	}
	if len(b) != 0 {
		return fmt.Errorf("UNSUPPORTED: trailing FLAC comment bytes")
	}
	return nil
}

func flacPicture(b []byte) ([]byte, error) {
	if len(b) < 4 {
		return nil, fmt.Errorf("UNSUPPORTED: truncated FLAC picture")
	}
	mime, b, err := sized(b[4:], binary.BigEndian)
	if err != nil || string(mime) == "-->" {
		return nil, fmt.Errorf("UNSUPPORTED: linked or invalid FLAC picture")
	}
	_, b, err = sized(b, binary.BigEndian)
	if err != nil {
		return nil, err
	}
	if len(b) < 16 {
		return nil, fmt.Errorf("UNSUPPORTED: missing FLAC picture dimensions")
	}
	picture, remainder, err := sized(b[16:], binary.BigEndian)
	if err != nil || len(remainder) != 0 {
		return nil, fmt.Errorf("UNSUPPORTED: invalid FLAC picture data")
	}
	return picture, nil
}

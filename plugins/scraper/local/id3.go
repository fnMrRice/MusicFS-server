package scraperlocal

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func (e *Engine) id3(ctx context.Context, r enrichment.Resource, reader io.ReaderAt, header []byte) error {
	version := header[3]
	if (version != 3 && version != 4) || header[4] != 0 || header[5]&0x0f != 0 || (version == 3 && header[5]&0x10 != 0) {
		return fmt.Errorf("UNSUPPORTED: unsupported ID3 header")
	}
	size, err := syncSafe(header[6:10])
	if err != nil {
		return err
	}
	body, err := readBounded(ctx, reader, r.SizeBytes, 10, int64(size), maxMetadataBytes)
	if err != nil {
		return err
	}
	if version == 4 && header[5]&0x10 != 0 {
		footer, err := readBounded(ctx, reader, r.SizeBytes, 10+int64(size), 10, 10)
		if err != nil || string(footer[:3]) != "3DI" || !bytes.Equal(footer[3:], header[3:]) {
			return fmt.Errorf("UNSUPPORTED: invalid ID3 footer")
		}
	}
	if version == 3 && header[5]&0x80 != 0 {
		body = undoUnsynchronization(body)
	}
	if header[5]&0x40 != 0 {
		if len(body) < 4 {
			return fmt.Errorf("UNSUPPORTED: truncated ID3 extended header")
		}
		extended := int(binary.BigEndian.Uint32(body[:4])) + 4
		if version == 4 {
			extended, err = syncSafe(body[:4])
			if err != nil {
				return err
			}
		}
		if extended < 6 || extended > len(body) {
			return fmt.Errorf("UNSUPPORTED: invalid ID3 extended header size")
		}
		body = body[extended:]
	}
	for frames := 0; len(body) > 0; frames++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if frames >= 4096 {
			return fmt.Errorf("RATE_LIMITED: too many ID3 frames")
		}
		if body[0] == 0 {
			if len(bytes.Trim(body, "\x00")) != 0 {
				return fmt.Errorf("UNSUPPORTED: invalid ID3 padding")
			}
			break
		}
		if len(body) < 10 {
			return fmt.Errorf("UNSUPPORTED: truncated ID3 frame")
		}
		id := string(body[:4])
		for _, char := range id {
			if !((char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
				return fmt.Errorf("UNSUPPORTED: invalid ID3 frame identity")
			}
		}
		length := int(binary.BigEndian.Uint32(body[4:8]))
		if version == 4 {
			length, err = syncSafe(body[4:8])
			if err != nil {
				return err
			}
		}
		if length < 1 || length > len(body)-10 {
			return fmt.Errorf("UNSUPPORTED: invalid ID3 frame length")
		}
		flags := body[9]
		payload := body[10 : 10+length]
		body = body[10+length:]
		if id != "USLT" && id != "APIC" {
			continue
		}
		field := "lyrics"
		if id == "APIC" {
			field = "artwork"
		}
		var parseErr error
		if version == 3 && flags != 0 || version == 4 && flags&^byte(0x03) != 0 {
			parseErr = fmt.Errorf("UNSUPPORTED: compressed or encrypted ID3 frame")
		} else {
			if version == 4 && (flags&2 != 0 || header[5]&0x80 != 0) {
				payload = undoUnsynchronization(payload)
			}
			if version == 4 && flags&1 != 0 {
				if len(payload) < 4 {
					parseErr = fmt.Errorf("UNSUPPORTED: truncated ID3 length indicator")
				} else {
					declared, err := syncSafe(payload[:4])
					payload = payload[4:]
					if err != nil || declared != len(payload) {
						parseErr = fmt.Errorf("UNSUPPORTED: invalid ID3 length indicator")
					}
				}
			}
		}
		language := "und"
		var data []byte
		if parseErr == nil {
			if id == "USLT" {
				data, language, parseErr = id3Lyrics(payload, version)
			} else {
				data, parseErr = id3Picture(payload, version)
			}
		}
		if err = e.add(r, field, language, "embedded", id, data, parseErr); err != nil {
			return err
		}
	}
	return nil
}

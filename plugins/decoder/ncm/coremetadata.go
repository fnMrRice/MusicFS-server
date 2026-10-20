package ncmdecoder

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

type Metadata struct {
	Format     string   `json:"format"`
	MIME       string   `json:"mime"`
	Title      string   `json:"title,omitempty"`
	Album      string   `json:"album,omitempty"`
	Artists    []string `json:"artists,omitempty"`
	DurationMs *int64   `json:"durationMs,omitempty"`
	BitrateBps *int64   `json:"bitrateBps,omitempty"`
}

func (r *Reader) Metadata() Metadata {
	m := r.metadata
	m.Artists = append([]string(nil), m.Artists...)
	if m.DurationMs != nil {
		v := *m.DurationMs
		m.DurationMs = &v
	}
	if m.BitrateBps != nil {
		v := *m.BitrateBps
		m.BitrateBps = &v
	}
	return m
}

func decodeMetadata(encrypted []byte) (Metadata, error) {
	var result Metadata
	if len(encrypted) == 0 {
		return result, nil
	}
	for i := range encrypted {
		encrypted[i] ^= 0x63
	}
	prefix := []byte("163 key(Don't modify):")
	if !bytes.HasPrefix(encrypted, prefix) {
		return result, fmt.Errorf("UNSUPPORTED: invalid NCM metadata prefix")
	}
	encoded := encrypted[len(prefix):]
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Strict().Decode(decoded, encoded)
	if err != nil {
		return result, fmt.Errorf("UNSUPPORTED: invalid NCM metadata encoding")
	}
	plain, err := decryptBlocks(decoded[:n], metadataWrappingKey)
	clear(decoded)
	if err != nil {
		return result, err
	}
	defer clear(plain)
	if !bytes.HasPrefix(plain, []byte("music:")) {
		return result, fmt.Errorf("UNSUPPORTED: invalid NCM metadata envelope")
	}
	body := plain[6:]
	if !utf8.Valid(body) {
		return result, fmt.Errorf("UNSUPPORTED: invalid NCM metadata text")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return result, fmt.Errorf("UNSUPPORTED: invalid NCM metadata JSON")
	}
	result.Title = optionalText(fields["musicName"])
	result.Album = optionalText(fields["album"])
	result.DurationMs = optionalPositive(fields["duration"])
	result.BitrateBps = optionalPositive(fields["bitrate"])
	var artists []json.RawMessage
	if json.Unmarshal(fields["artist"], &artists) == nil && len(artists) <= 256 {
		for _, artist := range artists {
			var pair []json.RawMessage
			name := ""
			if json.Unmarshal(artist, &pair) == nil && len(pair) > 0 {
				name = optionalText(pair[0])
			} else {
				name = optionalText(artist)
			}
			if name != "" {
				result.Artists = append(result.Artists, name)
			}
		}
	}
	return result, nil
}

func optionalText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil || len(text) > 4096 {
		return ""
	}
	return text
}

func optionalPositive(raw json.RawMessage) *int64 {
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return nil
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}

package ncmdecoder

import (
	"encoding/json"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/internal/neteasetag"
)

type Metadata struct {
	NetEaseID  string   `json:"neteaseId,omitempty"`
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
	fields, err := neteasetag.Parse(encrypted)
	if err != nil {
		return result, err
	}
	result.NetEaseID = neteasetag.SongID(fields)
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

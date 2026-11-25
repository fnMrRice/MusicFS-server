package ncmdecoder

import (
	"encoding/json"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/internal/neteasetag"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
)

type Metadata struct {
	AlbumRelease *audiotypes.AlbumRelease `json:"albumRelease,omitempty"`
	NetEaseID    string                   `json:"neteaseId,omitempty"`
	Format       string                   `json:"format"`
	MIME         string                   `json:"mime"`
	Title        string                   `json:"title,omitempty"`
	Album        string                   `json:"album,omitempty"`
	Artists      []string                 `json:"artists,omitempty"`
	DurationMs   *int64                   `json:"durationMs,omitempty"`
	BitrateBps   *int64                   `json:"bitrateBps,omitempty"`
}

func (r *Reader) Metadata() Metadata {
	return cloneMetadata(r.metadata)
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
	result.Title = neteasetag.Text(fields["musicName"])
	result.Album = neteasetag.Text(fields["album"])
	result.DurationMs = optionalPositive(fields["duration"])
	result.BitrateBps = optionalPositive(fields["bitrate"])
	result.Artists = neteasetag.Artists(fields)
	result.AlbumRelease = neteasetag.Release(fields, result.Title, result.Album, result.Artists)
	return result, nil
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

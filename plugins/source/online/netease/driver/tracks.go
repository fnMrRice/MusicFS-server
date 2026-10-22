package driver

import (
	"context"
	"encoding/json"
	"net/url"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type artist struct {
	Name string `json:"name"`
}
type album struct {
	Name string `json:"name"`
}
type neteaseSong struct {
	ID           json.Number `json:"id"`
	Name         string      `json:"name"`
	Artists      []artist    `json:"artists"`
	Album        album       `json:"album"`
	Duration     *int64      `json:"duration"`
	AR           []artist    `json:"ar"`
	AL           album       `json:"al"`
	DT           *int64      `json:"dt"`
	Aliases      []string    `json:"alia"`
	Translations []string    `json:"tns"`
}

func (s neteaseSong) track(detail bool) (Track, error) {
	if !numericID.MatchString(s.ID.String()) {
		return Track{}, upstreamInvalid()
	}
	t := Track{ExternalID: s.ID.String(), Artists: []string{}, PlaybackAvailability: "unknown"}
	t.SourceURL = "https://music.163.com/song?id=" + t.ExternalID
	var err error
	t.Title, err = textValue(s.Name, 4096)
	if err != nil || t.Title == "" {
		return Track{}, upstreamInvalid()
	}
	artists, a, duration := s.Artists, s.Album, s.Duration
	if detail {
		artists, a, duration = s.AR, s.AL, s.DT
	}
	if len(artists) > 128 {
		return Track{}, upstreamInvalid()
	}
	for _, value := range artists {
		name, err := textValue(value.Name, 4096)
		if err != nil {
			return Track{}, err
		}
		if name != "" {
			t.Artists = append(t.Artists, name)
		}
	}
	t.Album, err = textValue(a.Name, 4096)
	if err != nil {
		return Track{}, err
	}
	if duration != nil && (*duration < 0 || *duration > 7*24*60*60*1000) {
		return Track{}, upstreamInvalid()
	}
	if duration != nil && *duration > 0 {
		value := *duration
		t.DurationMs = &value
	}
	t.MetadataDigest = digest(t)
	return t, nil
}

func (c *Client) Metadata(ctx context.Context, externalID string) (Track, error) {
	song, err := c.metadataSong(ctx, externalID)
	if err != nil {
		return Track{}, err
	}
	return song.track(true)
}

func (c *Client) metadataSong(ctx context.Context, externalID string) (neteaseSong, error) {
	if err := c.Require(ctx); err != nil {
		return neteaseSong{}, err
	}
	if !numericID.MatchString(externalID) {
		return neteaseSong{}, domain.Fail("INVALID_ARGUMENT", "Track identifier is invalid")
	}
	var response struct {
		Code  *int          `json:"code"`
		Songs []neteaseSong `json:"songs"`
	}
	param := `[{"id":` + externalID + `}]`
	if err := c.Get(ctx, "/api/v3/song/detail", url.Values{"c": {param}}, &response); err != nil {
		return neteaseSong{}, err
	}
	if response.Code == nil {
		return neteaseSong{}, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return neteaseSong{}, err
	}
	if len(response.Songs) == 0 {
		return neteaseSong{}, domain.Fail("NOT_FOUND", "Online track metadata was not found")
	}
	if len(response.Songs) != 1 || response.Songs[0].ID.String() != externalID {
		return neteaseSong{}, upstreamInvalid()
	}
	return response.Songs[0], nil
}

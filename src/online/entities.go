package online

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Album struct {
	ExternalID string   `json:"externalId"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	TrackCount *int64   `json:"trackCount"`
	CoverURL   *string  `json:"coverUrl"`
	SourceURL  string   `json:"sourceUrl"`
}
type Artist struct {
	ExternalID string  `json:"externalId"`
	Name       string  `json:"name"`
	AlbumCount *int64  `json:"albumCount"`
	TrackCount *int64  `json:"trackCount"`
	ImageURL   *string `json:"imageUrl"`
	SourceURL  string  `json:"sourceUrl"`
}

func SearchKind(kind string) bool {
	return kind == "tracks" || kind == "albums" || kind == "artists" || kind == "playlists"
}
func emptySearchPage(kind string) SearchPage {
	return SearchPage{Kind: kind, Tracks: []Track{}, Albums: []Album{}, Artists: []Artist{}, Playlists: []Playlist{}, Consistency: Unversioned, ObservedAt: time.Now().UTC()}
}

// Image URLs are display references, never server-side fetch destinations.
// Only the platform's public image hosts survive normalization.
func imageURL(raw string) (*string, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Hostname(), ".music.126.net") {
		return nil, upstreamInvalid()
	}
	u.Scheme = "https"
	value := u.String()
	return &value, nil
}
func validCount(n *int64) bool { return n == nil || *n >= 0 }

type neteaseAlbum struct {
	ID      json.Number `json:"id"`
	Name    string      `json:"name"`
	Artists []artist    `json:"artists"`
	Artist  artist      `json:"artist"`
	Size    *int64      `json:"size"`
	PicURL  string      `json:"picUrl"`
}

func (a neteaseAlbum) summary() (Album, error) {
	name, err := textValue(a.Name, 4096)
	if err != nil || name == "" || !numericID.MatchString(a.ID.String()) || !validCount(a.Size) || len(a.Artists) > 128 {
		return Album{}, upstreamInvalid()
	}
	cover, err := imageURL(a.PicURL)
	if err != nil {
		return Album{}, err
	}
	out := Album{ExternalID: a.ID.String(), Title: name, Artists: []string{}, TrackCount: a.Size, CoverURL: cover, SourceURL: "https://music.163.com/album?id=" + a.ID.String()}
	artists := a.Artists
	if len(artists) == 0 && a.Artist.Name != "" {
		artists = []artist{a.Artist}
	}
	for _, value := range artists {
		name, err := textValue(value.Name, 4096)
		if err != nil {
			return Album{}, err
		}
		if name != "" {
			out.Artists = append(out.Artists, name)
		}
	}
	return out, nil
}

type neteaseArtist struct {
	ID        json.Number `json:"id"`
	Name      string      `json:"name"`
	AlbumSize *int64      `json:"albumSize"`
	MusicSize *int64      `json:"musicSize"`
	PicURL    string      `json:"picUrl"`
}

func (a neteaseArtist) summary() (Artist, error) {
	name, err := textValue(a.Name, 4096)
	if err != nil || name == "" || !numericID.MatchString(a.ID.String()) || !validCount(a.AlbumSize) || !validCount(a.MusicSize) {
		return Artist{}, upstreamInvalid()
	}
	image, err := imageURL(a.PicURL)
	if err != nil {
		return Artist{}, err
	}
	return Artist{a.ID.String(), name, a.AlbumSize, a.MusicSize, image, "https://music.163.com/artist?id=" + a.ID.String()}, nil
}

func (c *Client) searchEntities(ctx context.Context, r SearchRequest) (SearchPage, error) {
	if err := c.requireNetease(ctx); err != nil {
		return SearchPage{}, err
	}
	offset, err := c.readCursor(r)
	if err != nil {
		return SearchPage{}, err
	}
	kind := map[string]string{"albums": "10", "artists": "100", "playlists": "1000"}[r.Kind]
	var response struct {
		Code   *int `json:"code"`
		Result *struct {
			Albums        []neteaseAlbum    `json:"albums"`
			Artists       []neteaseArtist   `json:"artists"`
			Playlists     []neteasePlaylist `json:"playlists"`
			AlbumCount    *int64            `json:"albumCount"`
			ArtistCount   *int64            `json:"artistCount"`
			PlaylistCount *int64            `json:"playlistCount"`
			HasMore       *bool             `json:"hasMore"`
		} `json:"result"`
	}
	if err = c.get(ctx, "/api/search/get", url.Values{"s": {r.Query}, "type": {kind}, "limit": {strconv.Itoa(r.Limit)}, "offset": {strconv.Itoa(offset)}}, &response); err != nil {
		return SearchPage{}, err
	}
	if response.Code == nil {
		return SearchPage{}, upstreamInvalid()
	}
	if err = c.businessError(*response.Code, 200); err != nil {
		return SearchPage{}, err
	}
	if response.Result == nil {
		return SearchPage{}, upstreamInvalid()
	}
	page := emptySearchPage(r.Kind)
	result := response.Result
	var count *int64
	switch r.Kind {
	case "albums":
		count = result.AlbumCount
		for _, raw := range result.Albums {
			v, e := raw.summary()
			if e != nil {
				return SearchPage{}, e
			}
			page.Albums = append(page.Albums, v)
		}
	case "artists":
		count = result.ArtistCount
		for _, raw := range result.Artists {
			v, e := raw.summary()
			if e != nil {
				return SearchPage{}, e
			}
			page.Artists = append(page.Artists, v)
		}
	case "playlists":
		count = result.PlaylistCount
		for _, raw := range result.Playlists {
			v, e := raw.summary()
			if e != nil {
				return SearchPage{}, e
			}
			page.Playlists = append(page.Playlists, v)
		}
	}
	n := len(page.Albums) + len(page.Artists) + len(page.Playlists)
	if count == nil || *count < 0 || n > r.Limit {
		return SearchPage{}, upstreamInvalid()
	}
	more := int64(offset+n) < *count
	if result.HasMore != nil {
		more = more && *result.HasMore
	}
	page.Truncated = more && (n == 0 || offset+n >= 10000)
	if more && !page.Truncated {
		value := c.writeCursor(r, offset+n)
		page.NextCursor = &value
	}
	return page, nil
}

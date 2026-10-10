package online

import (
	"context"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type EntityPageRequest struct {
	ExternalID string `json:"externalId"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
}

func (c *Client) requireEntity(ctx context.Context, id string) error {
	if err := c.requireNetease(ctx); err != nil {
		return err
	}
	if !numericID.MatchString(id) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid external entity identity")
	}
	return nil
}
func (c *Client) readAlbum(ctx context.Context, id string) (Album, []Track, error) {
	if err := c.requireEntity(ctx, id); err != nil {
		return Album{}, nil, err
	}
	var response struct {
		Code  *int           `json:"code"`
		Album *neteaseAlbum  `json:"album"`
		Songs *[]neteaseSong `json:"songs"`
	}
	if err := c.get(ctx, "/api/v1/album/"+id, nil, &response); err != nil {
		return Album{}, nil, err
	}
	if response.Code == nil {
		return Album{}, nil, upstreamInvalid()
	}
	if err := c.businessError(*response.Code, 200); err != nil {
		return Album{}, nil, err
	}
	if response.Album == nil {
		return Album{}, nil, domain.Fail("NOT_FOUND", "Public album not found")
	}
	if response.Album.ID.String() != id || response.Songs == nil || len(*response.Songs) > MaxPlaylistEntries {
		return Album{}, nil, upstreamInvalid()
	}
	a, err := response.Album.summary()
	if err != nil {
		return Album{}, nil, err
	}
	tracks := make([]Track, 0, len(*response.Songs))
	for _, song := range *response.Songs {
		track, err := song.track(true)
		if err != nil {
			return Album{}, nil, err
		}
		tracks = append(tracks, track)
	}
	return a, tracks, nil
}
func (c *Client) Album(ctx context.Context, id string) (Album, error) {
	a, _, err := c.readAlbum(ctx, id)
	return a, err
}
func (c *Client) Artist(ctx context.Context, id string) (Artist, error) {
	if err := c.requireEntity(ctx, id); err != nil {
		return Artist{}, err
	}
	var response struct {
		Code   *int           `json:"code"`
		Artist *neteaseArtist `json:"artist"`
	}
	if err := c.get(ctx, "/api/artist/"+id, nil, &response); err != nil {
		return Artist{}, err
	}
	if response.Code == nil {
		return Artist{}, upstreamInvalid()
	}
	if err := c.businessError(*response.Code, 200); err != nil {
		return Artist{}, err
	}
	if response.Artist == nil {
		return Artist{}, domain.Fail("NOT_FOUND", "Public artist not found")
	}
	if response.Artist.ID.String() != id {
		return Artist{}, upstreamInvalid()
	}
	return response.Artist.summary()
}
func entityPageBinding(kind string, r EntityPageRequest) (SearchRequest, error) {
	if !numericID.MatchString(r.ExternalID) || r.Limit < 1 || r.Limit > MaxPageItems || len(r.Cursor) > 1024 {
		return SearchRequest{}, domain.Fail("INVALID_ARGUMENT", "Invalid entity page")
	}
	return SearchRequest{Kind: kind, Query: r.ExternalID, Limit: r.Limit, Cursor: r.Cursor}, nil
}
func (c *Client) AlbumTracks(ctx context.Context, r EntityPageRequest) (SearchPage, error) {
	binding, err := entityPageBinding("albumTracks", r)
	if err != nil {
		return SearchPage{}, err
	}
	token, err := c.readCursorToken(binding)
	if err != nil {
		return SearchPage{}, err
	}
	a, tracks, err := c.readAlbum(ctx, r.ExternalID)
	if err != nil {
		return SearchPage{}, err
	}
	// CDN image hosts vary between otherwise identical observations.
	// Artwork is display-only and cannot invalidate the track sequence.
	identity := a
	identity.CoverURL = nil
	version := digest(struct {
		Album  Album
		Tracks []Track
	}{identity, tracks})
	if r.Cursor != "" && token.Version != version {
		return SearchPage{}, domain.Fail("CURSOR_EXPIRED", "Album observation changed; refresh the first page")
	}
	if token.Offset > len(tracks) {
		return SearchPage{}, invalidCursor()
	}
	end := min(token.Offset+r.Limit, len(tracks))
	page := emptySearchPage("tracks")
	page.Tracks = append(page.Tracks, tracks[token.Offset:end]...)
	if end < len(tracks) {
		cursor := c.writeVersionCursor(binding, end, version)
		page.NextCursor = &cursor
	} else {
		page.Truncated = a.TrackCount == nil || *a.TrackCount != int64(len(tracks))
	}
	return page, nil
}
func (c *Client) ArtistAlbums(ctx context.Context, r EntityPageRequest) (SearchPage, error) {
	if err := c.requireEntity(ctx, r.ExternalID); err != nil {
		return SearchPage{}, err
	}
	binding, err := entityPageBinding("artistAlbums", r)
	if err != nil {
		return SearchPage{}, err
	}
	offset, err := c.readCursor(binding)
	if err != nil {
		return SearchPage{}, err
	}
	var response struct {
		Code   *int           `json:"code"`
		Artist *neteaseArtist `json:"artist"`
		Albums []neteaseAlbum `json:"hotAlbums"`
		More   *bool          `json:"more"`
	}
	if err = c.get(ctx, "/api/artist/albums/"+r.ExternalID, url.Values{"limit": {strconv.Itoa(r.Limit)}, "offset": {strconv.Itoa(offset)}}, &response); err != nil {
		return SearchPage{}, err
	}
	if response.Code == nil {
		return SearchPage{}, upstreamInvalid()
	}
	if err = c.businessError(*response.Code, 200); err != nil {
		return SearchPage{}, err
	}
	if response.Artist == nil || response.Artist.ID.String() != r.ExternalID || response.More == nil || len(response.Albums) > r.Limit {
		return SearchPage{}, upstreamInvalid()
	}
	page := emptySearchPage("albums")
	for _, raw := range response.Albums {
		a, err := raw.summary()
		if err != nil {
			return SearchPage{}, err
		}
		page.Albums = append(page.Albums, a)
	}
	c.finishEntityPage(&page, binding, offset, len(page.Albums), *response.More)
	return page, nil
}
func (c *Client) ArtistTracks(ctx context.Context, r EntityPageRequest) (SearchPage, error) {
	if err := c.requireEntity(ctx, r.ExternalID); err != nil {
		return SearchPage{}, err
	}
	binding, err := entityPageBinding("artistTracks", r)
	if err != nil {
		return SearchPage{}, err
	}
	offset, err := c.readCursor(binding)
	if err != nil {
		return SearchPage{}, err
	}
	var response struct {
		Code  *int          `json:"code"`
		Songs []neteaseSong `json:"songs"`
		More  *bool         `json:"more"`
		Total *int64        `json:"total"`
	}
	if err = c.get(ctx, "/api/v1/artist/songs", url.Values{"id": {r.ExternalID}, "limit": {strconv.Itoa(r.Limit)}, "offset": {strconv.Itoa(offset)}, "order": {"hot"}}, &response); err != nil {
		return SearchPage{}, err
	}
	if response.Code == nil {
		return SearchPage{}, upstreamInvalid()
	}
	if err = c.businessError(*response.Code, 200); err != nil {
		return SearchPage{}, err
	}
	if response.More == nil || response.Total == nil || *response.Total < 0 || len(response.Songs) > r.Limit {
		return SearchPage{}, upstreamInvalid()
	}
	page := emptySearchPage("tracks")
	for _, raw := range response.Songs {
		track, err := raw.track(false)
		if err != nil {
			return SearchPage{}, err
		}
		page.Tracks = append(page.Tracks, track)
	}
	c.finishEntityPage(&page, binding, offset, len(page.Tracks), *response.More)
	return page, nil
}
func (c *Client) finishEntityPage(page *SearchPage, binding SearchRequest, offset, n int, more bool) {
	page.Truncated = more && (n == 0 || offset+n >= 10000)
	if more && !page.Truncated {
		cursor := c.writeCursor(binding, offset+n)
		page.NextCursor = &cursor
	}
}

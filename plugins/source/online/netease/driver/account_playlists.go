package driver

import (
	"context"
	"net/url"
	"strconv"
	"time"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// Playlists lists only the currently validated platform account's collection.
// The account lock also binds every cursor to its login generation.
func (c *Client) Playlists(ctx context.Context, request PlaylistsRequest) (PlaylistsPage, error) {

	if err := c.Require(ctx); err != nil {
		return PlaylistsPage{}, err
	}
	if request.Limit < 1 || request.Limit > MaxPageItems || len(request.Cursor) > 1024 {
		return PlaylistsPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid account playlist page")
	}
	accountID := c.Secret.AccountID
	if !numericID.MatchString(accountID) {
		return PlaylistsPage{}, platformLoginRequired()
	}
	binding := SearchRequest{Kind: "account.playlists", Limit: request.Limit, Cursor: request.Cursor}
	offset, err := c.ReadCursor(binding)
	if err != nil {
		return PlaylistsPage{}, err
	}
	var response struct {
		Code      *int               `json:"code"`
		Playlists *[]neteasePlaylist `json:"playlist"`
		More      *bool              `json:"more"`
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return PlaylistsPage{}, err
	}
	if err := c.Get(ctx, "/api/user/playlist", url.Values{"uid": {accountID}, "limit": {strconv.Itoa(request.Limit)}, "offset": {strconv.Itoa(offset)}, "includeVideo": {"true"}}, &response); err != nil {
		return PlaylistsPage{}, err
	}
	if response.Code == nil {
		return PlaylistsPage{}, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return PlaylistsPage{}, err
	}
	if response.Playlists == nil || response.More == nil || len(*response.Playlists) > request.Limit || (*response.More && len(*response.Playlists) == 0) {
		return PlaylistsPage{}, upstreamInvalid()
	}
	page := PlaylistsPage{Items: []Playlist{}, Consistency: Unversioned, ObservedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for _, raw := range *response.Playlists {
		playlist, err := raw.summary()
		if err != nil || seen[playlist.ExternalID] {
			return PlaylistsPage{}, upstreamInvalid()
		}
		seen[playlist.ExternalID] = true
		playlist.Writable = raw.ownedBy(accountID)
		page.Items = append(page.Items, playlist)
	}
	if *response.More {
		if offset+len(page.Items) >= MaxPlaylistEntries {
			return PlaylistsPage{}, domain.Fail("UNSUPPORTED", "Account playlist collection exceeds the supported paging limit")
		}
		cursor := c.WriteCursor(binding, offset+len(page.Items))
		page.NextCursor = &cursor
	}
	return page, nil

}

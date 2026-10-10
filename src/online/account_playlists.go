package online

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type PlaylistsRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type PlaylistsPage struct {
	Items       []Playlist `json:"items"`
	NextCursor  *string    `json:"nextCursor"`
	Consistency string     `json:"consistency"`
	ObservedAt  time.Time  `json:"observedAt"`
}

// Playlists lists only the currently validated platform account's collection.
// The account lock also binds every cursor to its login generation.
func (c *Client) Playlists(ctx context.Context, request PlaylistsRequest) (PlaylistsPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (PlaylistsPage, error) {
		if err := c.requireNetease(ctx); err != nil {
			return PlaylistsPage{}, err
		}
		if request.Limit < 1 || request.Limit > MaxPageItems || len(request.Cursor) > 1024 {
			return PlaylistsPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid account playlist page")
		}
		accountID := c.account.secret.AccountID
		if !numericID.MatchString(accountID) {
			return PlaylistsPage{}, platformLoginRequired()
		}
		binding := SearchRequest{Kind: "account.playlists", Limit: request.Limit, Cursor: request.Cursor}
		offset, err := c.readCursor(binding)
		if err != nil {
			return PlaylistsPage{}, err
		}
		var response struct {
			Code      *int               `json:"code"`
			Playlists *[]neteasePlaylist `json:"playlist"`
			More      *bool              `json:"more"`
		}
		if err := c.get(ctx, "/api/user/playlist", url.Values{"uid": {accountID}, "limit": {strconv.Itoa(request.Limit)}, "offset": {strconv.Itoa(offset)}, "includeVideo": {"true"}}, &response); err != nil {
			return PlaylistsPage{}, err
		}
		if response.Code == nil {
			return PlaylistsPage{}, upstreamInvalid()
		}
		if err := c.businessError(*response.Code, 200); err != nil {
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
			page.Items = append(page.Items, playlist)
		}
		if *response.More {
			if offset+len(page.Items) >= MaxPlaylistEntries {
				return PlaylistsPage{}, domain.Fail("UNSUPPORTED", "Account playlist collection exceeds the supported paging limit")
			}
			cursor := c.writeCursor(binding, offset+len(page.Items))
			page.NextCursor = &cursor
		}
		return page, nil
	})
}

func ValidatePlaylists(platform string, page PlaylistsPage, limit int) error {
	if platform != Netease || limit < 1 || limit > MaxPageItems || page.Items == nil || len(page.Items) > limit || page.Consistency != Unversioned || page.ObservedAt.IsZero() {
		return invalidResult()
	}
	if page.NextCursor != nil && (len(*page.NextCursor) < 1 || len(*page.NextCursor) > 1024 || len(page.Items) == 0 || strings.ContainsAny(*page.NextCursor, " \r\n\t")) {
		return invalidResult()
	}
	seen := map[string]bool{}
	for _, playlist := range page.Items {
		if validatePlaylist(platform, playlist) != nil || seen[playlist.ExternalID] {
			return invalidResult()
		}
		seen[playlist.ExternalID] = true
	}
	return nil
}

package driver

import (
	"context"
	"net/url"
	"strconv"
	"time"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type accountPlaylistRow struct {
	neteasePlaylist
	SpecialType int `json:"specialType"`
}

// Playlists 读取当前已校验账号的歌单，游标绑定该账号和登录代次。
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
	token, err := c.ReadCursorToken(binding)
	if err != nil {
		return PlaylistsPage{}, err
	}
	// 网易云在首页额外插入“我喜欢的音乐”。游标记录已展示总数，上游偏移只计算普通歌单。
	pinnedID := token.Version
	offset := token.Offset
	if pinnedID != "" {
		offset--
	}
	var response struct {
		Code      *int                  `json:"code"`
		Playlists *[]accountPlaylistRow `json:"playlist"`
		More      *bool                 `json:"more"`
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
	if response.Playlists == nil || response.More == nil {
		return PlaylistsPage{}, upstreamInvalid()
	}
	rows := *response.Playlists
	hasPinned := offset == 0 && len(rows) > 0 && rows[0].SpecialType == 5
	maxRows := request.Limit
	if hasPinned {
		maxRows++
	}
	if len(rows) > maxRows {
		return PlaylistsPage{}, upstreamInvalid()
	}
	// 每页一张时，第二页仍从上游首页读取，并跳过已展示的同一张置顶歌单。
	skipPinned := offset == 0 && pinnedID != ""
	if skipPinned && (!hasPinned || rows[0].ID.String() != pinnedID) {
		return PlaylistsPage{}, domain.Fail("CURSOR_EXPIRED", "Pinned playlist changed; refresh the first page")
	}
	if hasPinned {
		pinnedID = rows[0].ID.String()
	}
	page := PlaylistsPage{Items: []Playlist{}, Consistency: Unversioned, ObservedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for _, raw := range rows {
		playlist, err := raw.summary()
		if err != nil || seen[playlist.ExternalID] {
			return PlaylistsPage{}, upstreamInvalid()
		}
		seen[playlist.ExternalID] = true
		playlist.Writable = raw.ownedBy(accountID)
		page.Items = append(page.Items, playlist)
	}
	if skipPinned {
		page.Items = page.Items[1:]
	}
	more := *response.More || len(page.Items) > request.Limit
	page.Items = page.Items[:min(len(page.Items), request.Limit)]
	next := token.Offset + len(page.Items)
	if next > MaxPlaylistEntries || (more && next == MaxPlaylistEntries) {
		return PlaylistsPage{}, domain.Fail("UNSUPPORTED", "Account playlist collection exceeds the supported paging limit")
	}
	if more {
		if len(page.Items) == 0 {
			return PlaylistsPage{}, upstreamInvalid()
		}
		cursor := c.WriteVersionCursor(binding, next, pinnedID)
		page.NextCursor = &cursor
	}
	return page, nil
}

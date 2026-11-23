package driver

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type accountPlaylistRow struct {
	neteasePlaylist
	SpecialType int `json:"specialType"`
}

// Offset 是上游普通歌单偏移，Skip 和 After 标记当前批次内已经展示的边界。
type accountPlaylistWindow struct {
	Offset int    `json:"offset"`
	Skip   int    `json:"skip,omitempty"`
	After  string `json:"after,omitempty"`
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
	var window accountPlaylistWindow
	if request.Cursor != "" && (json.Unmarshal([]byte(token.Version), &window) != nil ||
		window.Offset < 0 || window.Skip < 0 || window.Offset+window.Skip > token.Offset) {
		return PlaylistsPage{}, invalidCursor()
	}
	rows, more, err := c.accountPlaylistBatch(ctx, accountID, window.Offset)
	if err != nil {
		return PlaylistsPage{}, err
	}
	if window.Skip > len(rows) || (window.Skip > 0 && rows[window.Skip-1].ID.String() != window.After) {
		return PlaylistsPage{}, domain.Fail("CURSOR_EXPIRED", "Account playlists changed; refresh the first page")
	}
	end := min(window.Skip+request.Limit, len(rows))
	page := PlaylistsPage{Items: []Playlist{}, Consistency: Unversioned, ObservedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for i, raw := range rows {
		playlist, err := raw.summary()
		if err != nil || seen[playlist.ExternalID] {
			return PlaylistsPage{}, upstreamInvalid()
		}
		seen[playlist.ExternalID] = true
		if i >= window.Skip && i < end {
			playlist.Writable = raw.ownedBy(accountID)
			page.Items = append(page.Items, playlist)
		}
	}
	total := token.Offset + len(page.Items)
	hasNext := end < len(rows) || more
	if total > MaxPlaylistEntries || (hasNext && total == MaxPlaylistEntries) {
		return PlaylistsPage{}, domain.Fail("UNSUPPORTED", "Account playlist collection exceeds the supported paging limit")
	}
	if !hasNext {
		return page, nil
	}
	if len(page.Items) == 0 {
		return PlaylistsPage{}, upstreamInvalid()
	}
	if end < len(rows) {
		window.Skip, window.After = end, rows[end-1].ID.String()
	} else {
		// “我喜欢的音乐”只出现在首页，不计入下个上游批次的普通歌单偏移。
		nextOffset := window.Offset + len(rows)
		if window.Offset == 0 && rows[0].SpecialType == 5 {
			nextOffset--
		}
		if nextOffset <= window.Offset {
			return PlaylistsPage{}, upstreamInvalid()
		}
		window = accountPlaylistWindow{Offset: nextOffset}
	}
	state, _ := json.Marshal(window)
	cursor := c.WriteVersionCursor(binding, total, string(state))
	page.NextCursor = &cursor
	return page, nil
}

// 网易云可能返回多于请求条数的歌单，独立读取上游批次，再按客户端页大小展示。
func (c *Client) accountPlaylistBatch(ctx context.Context, accountID string, offset int) ([]accountPlaylistRow, bool, error) {
	var response struct {
		Code      *int                  `json:"code"`
		Playlists *[]accountPlaylistRow `json:"playlist"`
		More      *bool                 `json:"more"`
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return nil, false, err
	}
	if err := c.Get(ctx, "/api/user/playlist", url.Values{"uid": {accountID}, "limit": {strconv.Itoa(MaxPageItems)}, "offset": {strconv.Itoa(offset)}, "includeVideo": {"true"}}, &response); err != nil {
		return nil, false, err
	}
	if response.Code == nil {
		return nil, false, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return nil, false, err
	}
	if response.Playlists == nil || response.More == nil {
		return nil, false, upstreamInvalid()
	}
	rows := *response.Playlists
	if len(rows) > MaxPlaylistEntries {
		return nil, false, domain.Fail("UNSUPPORTED", "Account playlist collection exceeds the supported paging limit")
	}
	if *response.More {
		if len(rows) == 0 {
			return nil, false, upstreamInvalid()
		}
		windowSize := MaxPageItems
		if offset == 0 && rows[0].SpecialType == 5 {
			windowSize++
		}
		rows = rows[:min(windowSize, len(rows))]
	}
	return rows, *response.More, nil
}

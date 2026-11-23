package onlinehost

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type accountPlaylistDirectory struct {
	Items      []online.Playlist `json:"items"`
	ObservedAt time.Time         `json:"observedAt"`
	Version    string            `json:"version"`
}

// BrowsePlaylists 按需从网易云的完整当日目录切页，其余来源继续使用插件分页。
func (s *Service) BrowsePlaylists(ctx context.Context, owner, id string, request online.PlaylistsRequest, refresh bool) (online.PlaylistsPage, error) {
	if request.Limit < 1 || request.Limit > online.MaxPageItems || len(request.Cursor) > 1024 {
		return online.PlaylistsPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid account playlist page")
	}
	if s.playlistCache == nil {
		return s.Playlists(ctx, owner, id, request)
	}
	runtime, err := s.Runtime(ctx, owner, id, "")
	if err != nil {
		return online.PlaylistsPage{}, err
	}
	if runtime.Platform != online.Netease {
		return s.Playlists(ctx, owner, id, request)
	}
	if refresh && request.Cursor != "" {
		return online.PlaylistsPage{}, domain.Fail("INVALID_ARGUMENT", "Refresh account playlists from the first page")
	}
	directory, err := dailyPlaylistValue(ctx, s, owner, id, "account-playlists", refresh, func(ctx context.Context) (accountPlaylistDirectory, error) {
		return s.readAccountPlaylistDirectory(ctx, owner, id)
	})
	if err != nil {
		return online.PlaylistsPage{}, err
	}
	size := len(directory.Items)
	offset, err := playlistOffset(request.Cursor, directory.Version, request.Limit, size)
	if err != nil {
		return online.PlaylistsPage{}, err
	}
	end := min(offset+request.Limit, size)
	return online.PlaylistsPage{
		Items: directory.Items[offset:end], ObservedAt: directory.ObservedAt, Consistency: online.Unversioned,
		NextCursor: nextPlaylistCursor(directory.Version, request.Limit, end, size),
	}, nil
}

func (s *Service) readAccountPlaylistDirectory(ctx context.Context, owner, id string) (accountPlaylistDirectory, error) {
	directory := accountPlaylistDirectory{Items: []online.Playlist{}, Version: online.Digest(domain.NewID())}
	request := online.PlaylistsRequest{Limit: online.MaxPageItems}
	seenIDs, seenCursors := map[string]bool{}, map[string]bool{}
	for {
		page, err := s.Playlists(ctx, owner, id, request)
		if err != nil {
			return accountPlaylistDirectory{}, err
		}
		if page.Items == nil || len(page.Items) > request.Limit || page.Consistency != online.Unversioned || page.ObservedAt.IsZero() {
			return accountPlaylistDirectory{}, domain.Fail("PROTOCOL_MISMATCH", "Invalid account playlist page")
		}
		if len(directory.Items)+len(page.Items) > online.MaxPlaylistEntries {
			return accountPlaylistDirectory{}, domain.Fail("UNSUPPORTED", "Account playlist directory exceeds its limit")
		}
		for _, item := range page.Items {
			if item.ExternalID == "" || seenIDs[item.ExternalID] {
				return accountPlaylistDirectory{}, domain.Fail("RESOURCE_CHANGED", "Account playlist directory changed while paging")
			}
			seenIDs[item.ExternalID] = true
		}
		directory.Items = append(directory.Items, page.Items...)
		directory.ObservedAt = page.ObservedAt
		if page.NextCursor == nil {
			return directory, nil
		}
		request.Cursor = *page.NextCursor
		if len(page.Items) == 0 || request.Cursor == "" || len(request.Cursor) > 1024 || seenCursors[request.Cursor] {
			return accountPlaylistDirectory{}, domain.Fail("PROTOCOL_MISMATCH", "Account playlist pagination did not advance")
		}
		seenCursors[request.Cursor] = true
	}
}

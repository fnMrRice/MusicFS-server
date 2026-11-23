package onlinehost

import (
	"context"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type playlistSnapshot struct {
	Observation online.PlaylistObservation `json:"observation"`
	Version     string                     `json:"version"`
}

func (s *Service) playlistSnapshot(ctx context.Context, owner, id, external string, scope playlistScope, refresh bool) (playlistSnapshot, error) {
	return cachedPlaylistValue(ctx, s, owner, id, scope, "playlist:"+external, "", refresh, func(ctx context.Context) (playlistSnapshot, error) {
		observation, err := s.ObservePlaylist(ctx, owner, id, external)
		return playlistSnapshot{observation, online.Digest(domain.NewID())}, err
	})
}

// BrowsePlaylist 只用于浏览。导入、关联与写回仍通过实时观察确认歌单身份和内容。
func (s *Service) BrowsePlaylist(ctx context.Context, owner, id, external string, refresh bool) (online.PlaylistObservation, error) {
	r, err := s.Runtime(ctx, owner, id, "")
	if err != nil {
		return online.PlaylistObservation{}, err
	}
	if r.Platform != online.Netease || s.playlistCache == nil {
		return s.ObservePlaylist(ctx, owner, id, external)
	}
	if !externalID.MatchString(external) {
		return online.PlaylistObservation{}, domain.Fail("INVALID_ARGUMENT", "Invalid external playlist identity")
	}
	scope, err := s.playlistScope(ctx, owner, id)
	if err != nil {
		return online.PlaylistObservation{}, err
	}
	snapshot, err := s.playlistSnapshot(ctx, owner, id, external, scope, refresh)
	return snapshot.Observation, err
}

func (s *Service) BrowsePlaylistTracks(ctx context.Context, owner, id string, request online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	var empty online.PlaylistTracksPage
	r, err := s.Runtime(ctx, owner, id, "")
	if err != nil {
		return empty, err
	}
	if r.Platform != online.Netease || s.playlistCache == nil {
		return s.PlaylistTracks(ctx, owner, id, request)
	}
	if !externalID.MatchString(request.ExternalID) || request.Limit < 1 || request.Limit > online.MaxPageItems || len(request.Cursor) > 1024 {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid playlist track page")
	}
	scope, err := s.playlistScope(ctx, owner, id)
	if err != nil {
		return empty, err
	}
	snapshot, err := s.playlistSnapshot(ctx, owner, id, request.ExternalID, scope, false)
	if err != nil {
		return empty, err
	}
	observation := snapshot.Observation
	size := len(observation.Entries)
	offset, err := playlistOffset(request.Cursor, snapshot.Version, request.Limit, size)
	if err != nil {
		return empty, err
	}
	end := min(offset+request.Limit, size)
	page := online.PlaylistTracksPage{
		Playlist: observation.Playlist, Items: []online.PlaylistTrack{}, MetadataDigest: observation.MetadataDigest,
		Complete: observation.Complete, Consistency: observation.Consistency, ObservedAt: observation.ObservedAt,
		NextCursor: nextPlaylistCursor(snapshot.Version, request.Limit, end, size),
	}
	// 按固定批次取资料，让不同分页大小复用同一批次，同时保留重复曲目与缺失位置。
	for start := offset - offset%online.MaxPageItems; start < end; start += online.MaxPageItems {
		entries := observation.Entries[start:min(start+online.MaxPageItems, size)]
		key := "tracks:" + request.ExternalID + ":" + strconv.Itoa(start)
		batch, err := cachedPlaylistValue(ctx, s, owner, id, scope, key, snapshot.Version, false, func(ctx context.Context) (online.TrackBatch, error) {
			ids := make([]string, len(entries))
			for i, entry := range entries {
				ids[i] = entry.ExternalTrackID
			}
			return s.MetadataBatch(ctx, owner, id, ids)
		})
		if err != nil {
			return empty, err
		}
		if len(batch.Items) != len(entries) {
			return empty, domain.Fail("PROTOCOL_MISMATCH", "Invalid playlist track batch size")
		}
		for pos := max(offset, start); pos < min(end, start+len(entries)); pos++ {
			page.Items = append(page.Items, online.PlaylistTrack{Position: pos, TrackLookup: batch.Items[pos-start]})
		}
	}
	if err := s.checkPlaylistScope(ctx, owner, id, scope); err != nil {
		return empty, err
	}
	return page, nil
}

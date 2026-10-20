package driver

import (
	"context"
	"encoding/json"
	"net/url"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// MetadataBatch uses a single upstream request, and preserves the caller's
// order and duplicate occurrences. Missing items are explicit, not dropped.
func (c *Client) MetadataBatch(ctx context.Context, ids []string) (TrackBatch, error) {
	if err := c.Require(ctx); err != nil {
		return TrackBatch{}, err
	}
	if len(ids) < 1 || len(ids) > MaxPageItems {
		return TrackBatch{}, domain.Fail("INVALID_ARGUMENT", "Track batch must contain 1 to 50 identities")
	}
	unique := map[string]bool{}
	params := []map[string]json.Number{}
	for _, id := range ids {
		if !numericID.MatchString(id) {
			return TrackBatch{}, domain.Fail("INVALID_ARGUMENT", "Invalid track identity")
		}
		if !unique[id] {
			unique[id] = true
			params = append(params, map[string]json.Number{"id": json.Number(id)})
		}
	}
	encoded, _ := json.Marshal(params)
	var response struct {
		Code  *int          `json:"code"`
		Songs []neteaseSong `json:"songs"`
	}
	if err := c.Get(ctx, "/api/v3/song/detail", url.Values{"c": {string(encoded)}}, &response); err != nil {
		return TrackBatch{}, err
	}
	if response.Code == nil {
		return TrackBatch{}, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return TrackBatch{}, err
	}
	if len(response.Songs) > len(unique) {
		return TrackBatch{}, upstreamInvalid()
	}
	tracks := map[string]Track{}
	for _, song := range response.Songs {
		track, err := song.track(true)
		if err != nil {
			return TrackBatch{}, err
		}
		if _, ok := tracks[track.ExternalID]; ok || !unique[track.ExternalID] {
			return TrackBatch{}, upstreamInvalid()
		}
		tracks[track.ExternalID] = track
	}
	result := TrackBatch{Items: []TrackLookup{}}
	for _, id := range ids {
		item := TrackLookup{ExternalID: id}
		if track, ok := tracks[id]; ok {
			item.Track = &track
		} else {
			code := "NOT_FOUND"
			item.ErrorCode = &code
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (c *Client) PlaylistTracks(ctx context.Context, r PlaylistTracksRequest) (PlaylistTracksPage, error) {
	if r.Limit < 1 || r.Limit > MaxPageItems || len(r.Cursor) > 1024 {
		return PlaylistTracksPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid playlist page")
	}
	observation, err := c.ObservePlaylist(ctx, r.ExternalID)
	if err != nil {
		return PlaylistTracksPage{}, err
	}
	// Every continuation rechecks the complete observed identity sequence.
	// A changed observation cannot silently append to an older page.
	binding := SearchRequest{Kind: "playlistTracks", Query: r.ExternalID, Limit: r.Limit, Cursor: r.Cursor}
	token, err := c.ReadCursorToken(binding)
	if err != nil {
		return PlaylistTracksPage{}, err
	}
	if r.Cursor != "" && token.Version != observation.MetadataDigest {
		return PlaylistTracksPage{}, domain.Fail("CURSOR_EXPIRED", "Playlist observation changed; refresh the first page")
	}
	offset := token.Offset
	if offset > len(observation.Entries) {
		return PlaylistTracksPage{}, invalidCursor()
	}
	page := PlaylistTracksPage{Playlist: observation.Playlist, Items: []PlaylistTrack{}, MetadataDigest: observation.MetadataDigest, Complete: observation.Complete, Consistency: Unversioned, ObservedAt: observation.ObservedAt}
	end := min(offset+r.Limit, len(observation.Entries))
	ids := []string{}
	for _, entry := range observation.Entries[offset:end] {
		ids = append(ids, entry.ExternalTrackID)
	}
	if len(ids) > 0 {
		if err = c.WaitForMetadata(ctx); err != nil {
			return PlaylistTracksPage{}, err
		}
		batch, err := c.MetadataBatch(ctx, ids)
		if err != nil {
			return PlaylistTracksPage{}, err
		}
		for i, item := range batch.Items {
			page.Items = append(page.Items, PlaylistTrack{Position: offset + i, TrackLookup: item})
		}
	}
	if end < len(observation.Entries) {
		cursor := c.WriteVersionCursor(binding, end, observation.MetadataDigest)
		page.NextCursor = &cursor
	}
	return page, nil
}

package bilibili

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func (c *Client) putSnapshot(s snapshot) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range c.snapshots {
		if time.Now().After(value.expires) {
			delete(c.snapshots, key)
		}
	}
	if len(c.snapshots) >= 8 {
		var oldestKey string
		var oldest time.Time
		for key, value := range c.snapshots {
			if oldestKey == "" || value.expires.Before(oldest) {
				oldestKey, oldest = key, value.expires
			}
		}
		delete(c.snapshots, oldestKey)
	}
	key := fault.NewID()
	c.snapshots[key] = s
	return key
}

func (c *Client) pagedSnapshot(cursor string) (snapshot, string, int, error) {
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return snapshot{}, "", 0, fault.Fail("INVALID_ARGUMENT", "Invalid Bilibili observation cursor")
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil || index < 0 {
		return snapshot{}, "", 0, fault.Fail("INVALID_ARGUMENT", "Invalid Bilibili observation cursor")
	}
	c.mu.Lock()
	s, ok := c.snapshots[parts[0]]
	c.mu.Unlock()
	if !ok || time.Now().After(s.expires) {
		return snapshot{}, "", 0, fault.Fail("CURSOR_EXPIRED", "Bilibili observation cursor has expired")
	}
	if index >= len(s.tracks) {
		return snapshot{}, "", 0, fault.Fail("INVALID_ARGUMENT", "Invalid Bilibili observation offset")
	}
	return s, parts[0], index, nil
}

func (c *Client) PlaylistTracks(ctx context.Context, r online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	result := online.PlaylistTracksPage{Items: []online.PlaylistTrack{}}
	if r.Limit < 1 || r.Limit > online.MaxPageItems {
		return result, fault.Fail("INVALID_ARGUMENT", "Page limit must be between 1 and 50")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var s snapshot
	var key string
	var start int
	var err error
	if r.Cursor == "" {
		s, err = c.observe(ctx, r.ExternalID)
		if err == nil {
			key = c.putSnapshot(s)
		}
	} else {
		s, key, start, err = c.pagedSnapshot(r.Cursor)
	}
	if err != nil {
		return result, err
	}
	ref, err := parseReference(r.ExternalID)
	if err != nil || ref.canonical() != s.observation.Playlist.ExternalID {
		return result, fault.Fail("INVALID_ARGUMENT", "Bilibili cursor belongs to another collection")
	}
	result.Playlist, result.MetadataDigest, result.Complete, result.Consistency, result.ObservedAt = s.observation.Playlist, s.observation.MetadataDigest, s.observation.Complete, online.Unversioned, s.observation.ObservedAt
	end := min(start+r.Limit, len(s.tracks))
	for i := start; i < end; i++ {
		result.Items = append(result.Items, online.PlaylistTrack{Position: i, TrackLookup: s.tracks[i]})
	}
	if end < len(s.tracks) {
		cursor := key + ":" + strconv.Itoa(end)
		result.NextCursor = &cursor
	}
	return result, nil
}

func (c *Client) Search(ctx context.Context, r online.SearchRequest) (online.SearchPage, error) {
	if r.Kind == "" {
		r.Kind = "tracks"
	}
	result := online.SearchPage{Kind: r.Kind, Tracks: []online.Track{}, Albums: []online.Album{}, Artists: []online.Artist{}, Playlists: []online.Playlist{}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}
	if r.Limit < 1 || r.Limit > online.MaxPageItems {
		return result, fault.Fail("INVALID_ARGUMENT", "Page limit must be between 1 and 50")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref, err := parseReference(r.Query)
	if err != nil {
		return result, err
	}
	if r.Kind == "playlists" {
		if r.Cursor != "" {
			return result, fault.Fail("INVALID_ARGUMENT", "Collection lookup does not accept a cursor")
		}
		p, err := c.playlistMetadata(ctx, ref)
		if err != nil {
			return result, err
		}
		result.Playlists = append(result.Playlists, p)
		return result, nil
	}
	if r.Kind != "tracks" || (ref.kind != "video" && ref.kind != "audio") {
		return result, fault.Fail("UNSUPPORTED", "Bilibili supports exact track and collection references only")
	}
	var s snapshot
	var key string
	var start int
	if r.Cursor != "" {
		s, key, start, err = c.pagedSnapshot(r.Cursor)
		if err != nil {
			return result, err
		}
		if s.observation.Playlist.ExternalID != r.Query {
			return result, fault.Fail("INVALID_ARGUMENT", "Bilibili cursor belongs to another lookup")
		}
	} else {
		s = snapshot{observation: online.PlaylistObservation{Playlist: online.Playlist{ExternalID: r.Query}}, expires: time.Now().Add(5 * time.Minute)}
		if ref.kind == "video" {
			info, err := c.video(ctx, ref)
			if err != nil {
				return result, err
			}
			for _, page := range info.Pages {
				if ref.cid != "" && strconv.FormatInt(page.CID, 10) != ref.cid || ref.selected && page.Page != ref.page {
					continue
				}
				track, err := videoTrack(info, page)
				if err != nil {
					return result, err
				}
				s.tracks = append(s.tracks, online.TrackLookup{ExternalID: track.ExternalID, Track: &track})
			}
			if len(s.tracks) == 0 {
				return result, fault.Fail("NOT_FOUND", "Bilibili video part was not found")
			}
		} else {
			track, err := c.Metadata(ctx, r.Query)
			if err != nil {
				return result, err
			}
			s.tracks = []online.TrackLookup{{ExternalID: track.ExternalID, Track: &track}}
		}
		key = c.putSnapshot(s)
	}
	end := min(start+r.Limit, len(s.tracks))
	for _, item := range s.tracks[start:end] {
		result.Tracks = append(result.Tracks, *item.Track)
	}
	if end < len(s.tracks) {
		cursor := key + ":" + strconv.Itoa(end)
		result.NextCursor = &cursor
	}
	return result, nil
}

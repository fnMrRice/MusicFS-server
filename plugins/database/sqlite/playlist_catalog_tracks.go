package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func importPlaylistTracks(ctx context.Context, tx *sql.Tx, owner string, b playlists.Binding, o playlists.Observation) error {
	metadata := make(map[string]online.TrackLookup, len(o.Tracks))
	for _, item := range o.Tracks {
		if _, exists := metadata[item.ExternalID]; exists {
			return playlists.Invalid()
		}
		metadata[item.ExternalID] = item
	}
	seen := map[string]bool{}
	trackIDs := []string{}
	for _, entry := range o.Entries {
		if seen[entry.ExternalTrackID] {
			continue
		}
		seen[entry.ExternalTrackID] = true
		item, ok := metadata[entry.ExternalTrackID]
		if !ok {
			return domain.Fail("PROTOCOL_MISMATCH", "Playlist member metadata is missing")
		}
		source, err := importPlaylistSource(ctx, tx, owner, b.InstanceID, item)
		if err != nil {
			return err
		}
		id, err := personalTrackID(ctx, tx, owner, source)
		if err != nil {
			return err
		}
		trackIDs = append(trackIDs, id)
	}
	if len(seen) != len(metadata) {
		return playlists.Invalid()
	}
	if len(seen) > 0 {
		if _, err := tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	var local string
	err := tx.QueryRowContext(ctx, `SELECT playlist_id FROM playlist_links WHERE owner=? AND instance_id=? AND account_id=? AND external_id=?`, owner, b.InstanceID, b.AccountID, b.ExternalID).Scan(&local)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if local == personal.Favorites {
		return importFavoriteTracks(ctx, tx, owner, trackIDs)
	}
	return importPersonalTracks(ctx, tx, owner, local, trackIDs)
}

func importPlaylistSource(ctx context.Context, tx *sql.Tx, owner, provider string, item online.TrackLookup) (string, error) {
	metadata, availability := item.Track, "available"
	if metadata == nil {
		if item.ErrorCode == nil || *item.ErrorCode != "NOT_FOUND" {
			return "", domain.Fail("TEMPORARY_FAILURE", "Playlist member metadata is unavailable")
		}
		// 平台已下架的成员保留编号；已知的歌名和署名仍然保留。
		var id string
		err := tx.QueryRowContext(ctx, `UPDATE tracks SET document=json_set(document,'$.availability','missing') WHERE owner=? AND provider=? AND external_id=? RETURNING id`, owner, provider, item.ExternalID).Scan(&id)
		if !errors.Is(err, sql.ErrNoRows) {
			return id, err
		}
		metadata, availability = &online.Track{ExternalID: item.ExternalID, Title: item.ExternalID}, "missing"
	} else if item.ErrorCode != nil || metadata.ExternalID != item.ExternalID || metadata.Title == "" {
		return "", domain.Fail("PROTOCOL_MISMATCH", "Invalid playlist member metadata")
	}
	source, err := publishOnlineTrack(ctx, tx, owner, provider, *metadata, availability)
	return source.ID, err
}

func importFavoriteTracks(ctx context.Context, tx *sql.Tx, owner string, ids []string) error {
	// 收藏按时间倒序显示。按远端顺序分配纳秒，保留已有收藏时间与指定来源。
	rows := make([][2]string, len(ids))
	now := time.Now().UTC()
	for i, id := range ids {
		rows[i] = [2]string{id, now.Add(-time.Duration(i)).Format("2006-01-02T15:04:05.000000000Z")}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,position,created_at,updated_at)
		SELECT ?,'favorites','',value->>'$[0]',value->>'$[0]',0,value->>'$[1]',value->>'$[1]'
		FROM json_each(?) WHERE true ORDER BY key ON CONFLICT(owner,playlist_id,binding_id,entry_id) DO NOTHING`, owner, string(raw))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM music_playlist_entries WHERE owner=? AND playlist_id='favorites' AND binding_id=''`, owner).Scan(&count); err != nil {
		return err
	}
	if count > personal.MaxFavorites {
		return domain.Fail("RATE_LIMITED", "Favorite collection limit reached")
	}
	return bumpPersonalRevision(ctx, tx, owner, personal.Favorites)
}

func importPersonalTracks(ctx context.Context, tx *sql.Tx, owner, id string, ids []string) error {
	p, err := readPersonalPlaylist(ctx, tx, owner, id)
	if err != nil {
		return err
	}
	builder, err := newPlaylistBuilder(ctx, tx, owner)
	if err != nil {
		return err
	}
	contributions, _, _, err := builder.contributions(p, 0)
	if err != nil {
		return err
	}
	refs := make([]playlists.SourceRef, len(contributions))
	for i, c := range contributions {
		refs[i] = c.Source
	}
	raw, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	err = musicRows(ctx, tx, `SELECT DISTINCT coalesce(json_extract(j.value,'$.trackId'),s.track_id,rs.track_id,'') FROM json_each(?) j
		LEFT JOIN remote_sources r ON r.owner=? AND r.id=json_extract(j.value,'$.remoteSourceId')
		LEFT JOIN tracks remote ON remote.owner=r.owner AND remote.provider=r.instance_id AND remote.external_id=r.external_id
		LEFT JOIN music_sources rs ON rs.id=remote.id AND rs.owner=r.owner
		LEFT JOIN music_sources s ON s.owner=? AND s.id=json_extract(j.value,'$.sourceTrackId')`, func(rows *sql.Rows) error {
		var track string
		if err := rows.Scan(&track); err != nil {
			return err
		}
		existing[track] = true
		return nil
	}, string(raw), owner, owner)
	if err != nil {
		return err
	}
	var added []playlists.LocalEntry
	for _, track := range ids {
		if !existing[track] {
			existing[track] = true
			added = append(added, playlists.LocalEntry{ID: domain.NewID(), Source: playlists.SourceRef{TrackID: &track}})
		}
	}
	if len(added) > 0 {
		index := len(p.Bindings) - 1
		if index < 0 || p.Bindings[index].Kind != "local" {
			p.Bindings = append(p.Bindings, playlists.PlaylistBinding{ID: domain.NewID(), Kind: "local"})
			index++
		}
		p.Bindings[index].Entries = append(p.Bindings[index].Entries, added...)
		if err = playlists.ValidateBindings(p.Bindings); err != nil {
			return err
		}
		p.RuleRevision++
		p.Stale = true
		if err = savePersonalPlaylist(ctx, tx, owner, &p, false); err != nil {
			return err
		}
		if err = saveMusicPlaylistEntries(ctx, tx, owner, p); err != nil {
			return err
		}
		if err = playlistAudit(ctx, tx, owner, p, "edit", nil); err != nil {
			return err
		}
	}
	_, err = builder.build(id, 0)
	return err
}

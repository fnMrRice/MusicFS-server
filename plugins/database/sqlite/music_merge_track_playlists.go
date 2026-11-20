package sqlite

import (
	"context"
	"database/sql"
)

// 隐藏规则的当前目标和已确认的来源范围都可能引用曲目。历史快照仍按其旧修订读取。
const trackInHiddenRule = `(json_extract(h.document,'$.Rule.source.trackId')=? OR EXISTS(
 SELECT 1 FROM json_each(h.document,'$.Allowed') a WHERE json_extract(a.value,'$.trackId')=?))`

func moveTrackPlaylistEntries(ctx context.Context, tx *sql.Tx, owner, target, donor string) error {
	_, err := tx.ExecContext(ctx, `UPDATE personal_playlists AS p SET rule_revision=rule_revision+1,
 document=json_set(document,'$.ruleRevision',rule_revision+1,'$.stale',json('true')) WHERE owner=? AND EXISTS(
 SELECT 1 FROM music_playlist_entries e WHERE e.owner=p.owner AND e.playlist_id=p.id AND e.binding_id<>'' AND e.track_id=?)`, owner, donor)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE personal_playlists AS p SET hide_revision=hide_revision+1,
 document=json_set(document,'$.hideRevision',hide_revision+1,'$.stale',json('true')) WHERE owner=? AND EXISTS(
 SELECT 1 FROM playlist_hidden_rules h WHERE h.owner=p.owner AND h.playlist_id=p.id AND `+trackInHiddenRule+`)`, owner, donor, donor)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE playlist_hidden_rules AS h SET document=json_set(
 CASE WHEN json_extract(document,'$.Rule.source.trackId')=? THEN json_set(document,'$.Rule.source.trackId',?) ELSE document END,
 '$.Allowed',json((SELECT json_group_array(json(CASE WHEN json_extract(a.value,'$.trackId')=?
 THEN json_set(a.value,'$.trackId',?) ELSE a.value END)) FROM json_each(document,'$.Allowed') a)))
 WHERE owner=? AND `+trackInHiddenRule, donor, target, donor, target, owner, donor, donor)
	if err != nil {
		return err
	}

	var favorite, recent bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=? AND playlist_id='favorites' AND binding_id='' AND track_id=?),
 EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=? AND playlist_id='recently-played' AND binding_id='' AND track_id=?)`, owner, donor, owner, donor).Scan(&favorite, &recent)
	if err != nil {
		return err
	}
	if favorite {
		// 收藏按规范曲目去重。主曲目已有收藏时保留该条目的时间和指定来源。
		_, err = tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at,play_count)
 SELECT owner,playlist_id,binding_id,?,?,forced_source_id,position,created_at,updated_at,play_count FROM music_playlist_entries
 WHERE owner=? AND playlist_id='favorites' AND binding_id='' AND track_id=? ON CONFLICT(owner,playlist_id,binding_id,entry_id) DO NOTHING`, target, target, owner, donor)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM music_playlist_entries WHERE owner=? AND playlist_id='favorites' AND binding_id='' AND track_id=?`, owner, donor); err != nil {
			return err
		}
		if err = bumpPersonalRevision(ctx, tx, owner, "favorites"); err != nil {
			return err
		}
	}
	if recent {
		if err = bumpPersonalRevision(ctx, tx, owner, "recently-played"); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE music_playlist_entries SET track_id=? WHERE owner=? AND track_id=?`, target, owner, donor)
	return err
}

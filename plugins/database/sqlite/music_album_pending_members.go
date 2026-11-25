package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type pendingAlbumSource struct {
	id, album, slot string
	credits         [][]string
	duration        int64
}

func (s pendingAlbumSource) matches(track albumScrapeTrack) bool {
	return nearScrapedDuration(track.track.Duration, s.duration, s.duration) &&
		slices.ContainsFunc(track.creditVariants, func(names []string) bool { return sameScrapedCredits(s.credits, names) })
}

// 手工重选同一发行曲表时，旧的可丢弃占位可以寻找本实例、本原始标签下刚被暂存的音源。
// 只用该份真实音源的时长与署名作证；其它发行或同录音的另一个来源不能替它提供证据。
func addPendingAlbumMembers(ctx context.Context, tx *sql.Tx, owner, album string, tracks []albumScrapeTrack, members map[string]*scrapedAlbumMember) error {
	const query = ordinaryMusicSources + `, target_labels AS (
 SELECT owner,tag_provider AS provider,tag_name FROM music_albums WHERE id=?
 UNION SELECT owner,provider,tag_name FROM music_album_tag_aliases WHERE album_id=?
), pending_albums AS MATERIALIZED (
 SELECT p.id,p.owner,p.tag_provider,p.unresolved_tag FROM target_labels l JOIN music_albums p
 ON p.owner=l.owner AND p.tag_provider=l.provider AND p.unresolved_tag=l.tag_name WHERE p.metadata_locked=0
)
 SELECT m.id,m.title,m.localized_titles,m.revision,t.id,p.id,
 json_extract(t.document,'$.artists'),CAST(json_extract(t.document,'$.durationMs') AS BIGINT)
 FROM pending_albums p JOIN music_album_source_tag_history h ON h.album_id=p.id AND h.owner=p.owner
 JOIN music_album_source_refs r ON r.source_id=h.source_id AND r.album_id=p.id AND r.origin_id='tag'
 JOIN music_sources s ON s.id=r.source_id AND s.owner=p.owner JOIN music_tracks m ON m.id=s.track_id AND m.owner=s.owner
 JOIN tracks t ON t.id=s.id AND t.owner=s.owner JOIN music_source_album_choices c ON c.id=t.id
 WHERE h.owner=? AND c.legacy_id=? AND c.override_id IS NULL AND c.namespace IS NULL
 AND p.unresolved_tag=c.album AND h.tag_name=c.album AND p.tag_provider=c.provider
 AND NOT EXISTS(SELECT 1 FROM music_album_release_identities i WHERE i.owner=h.owner AND i.album_id IN (p.id,c.legacy_id))
 AND NOT EXISTS(SELECT 1 FROM music_album_source_refs known JOIN music_observed_album_releases n ON n.id=known.source_id WHERE known.album_id=c.legacy_id)
 AND NOT EXISTS(SELECT 1 FROM music_manual_album_tracks u WHERE u.album_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_tag_aliases a WHERE a.album_id=p.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_source_tag_overrides o WHERE o.album_id=p.id)
 AND json_extract(t.document,'$.availability')='available' AND json_type(t.document,'$.artists')='array'
 AND json_type(t.document,'$.durationMs')='integer' AND ` + ordinaryMusicTrack
	pending := map[string]*scrapedAlbumMember{}
	names := map[string]bool{}
	err := musicRows(ctx, tx, query, func(row *sql.Rows) error {
		var m scrapedAlbumMember
		var source pendingAlbumSource
		var titles, rawArtists []byte
		if err := row.Scan(&m.id, &m.title, &titles, &m.revision, &source.id, &source.album, &rawArtists, &source.duration); err != nil {
			return err
		}
		if members[m.id] != nil || source.duration <= 0 {
			return nil
		}
		var artists []string
		if err := json.Unmarshal(rawArtists, &artists); err != nil {
			return err
		}
		for _, credit := range artists {
			for _, name := range scrapedArtistNames(credit) {
				names[name] = true
				source.credits = append(source.credits, []string{name})
			}
		}
		if len(source.credits) == 0 {
			return nil
		}
		target := pending[m.id]
		if target == nil {
			if err := json.Unmarshal(titles, &m.titles); err != nil {
				return err
			}
			target = &m
			pending[m.id] = target
		}
		target.pending = append(target.pending, source)
		return nil
	}, owner, album, album, owner, album)
	if err != nil || len(pending) == 0 {
		return err
	}
	aliases, err := evidenceArtistAliases(ctx, tx, owner, names)
	if err != nil {
		return err
	}
	for id, m := range pending {
		for i := range m.pending {
			source := &m.pending[i]
			for j, credit := range source.credits {
				source.credits[j] = append(credit, aliases[credit[0]]...)
			}
			for _, track := range tracks {
				if source.matches(track) {
					if source.slot != "" {
						source.slot = ""
						break
					}
					source.slot = track.slot
				}
			}
		}
		members[id] = m
	}
	return nil
}

// 保留自动标签的性质，后续文件提供真正的发行号时仍可更正。只移动该曲目中亲自匹配的来源。
func bindPendingAlbumSources(ctx context.Context, tx *sql.Tx, album string, member *scrapedAlbumMember, track albumScrapeTrack, changedAlbums map[string]bool) error {
	for _, s := range member.pending {
		if s.slot != track.slot {
			continue
		}
		for _, query := range []string{
			"UPDATE music_album_source_refs SET album_id=? WHERE source_id=? AND album_id=? AND origin_id='tag'",
			"UPDATE music_album_source_tag_history SET album_id=? WHERE source_id=? AND album_id=?",
		} {
			result, err := tx.ExecContext(ctx, query, album, s.id, s.album)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return domain.Fail("CONFLICT", "Pending album membership changed")
			}
		}
		if !changedAlbums[s.album] {
			if _, err := tx.ExecContext(ctx, "UPDATE music_albums SET revision=revision+1 WHERE id=?", s.album); err != nil {
				return err
			}
			changedAlbums[s.album] = true
		}
	}
	_, err := tx.ExecContext(ctx, "UPDATE music_tracks SET revision=revision+1 WHERE id=? AND revision=?", member.id, member.revision)
	return err
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func (s *Store) MusicAlbumTrackEvidence(ctx context.Context, owner, id, provider string) ([]music.AlbumTrackEvidence, error) {
	if !works.ValidID(id) || provider != "" && !works.ValidID(provider) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid album or provider identifier")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := readMusicAlbum(ctx, tx, owner, id); err != nil {
		return nil, err
	}
	return readAlbumTrackEvidence(ctx, tx, owner, id, provider)
}

// 原始标签按来源分组，避免将一个版本的曲名和另一个版本的署名拼成证据。
// 同一录音属于多个发行版时只采信当前专辑的来源；纯手工关联的录音使用其可见音源。
func readAlbumTrackEvidence(ctx context.Context, tx *sql.Tx, owner, id, provider string) ([]music.AlbumTrackEvidence, error) {
	const sources = ordinaryMusicSources + `, original AS (
 SELECT s.track_id,r.source_id FROM music_album_source_refs r JOIN music_sources s ON s.id=r.source_id
 WHERE r.album_id=? AND s.owner=?
), members AS (
 SELECT source_id FROM original UNION
 SELECT s.id FROM music_manual_album_tracks m JOIN music_sources s ON s.track_id=m.track_id
 WHERE m.album_id=? AND s.owner=? AND NOT EXISTS(SELECT 1 FROM original o WHERE o.track_id=m.track_id)
)
 SELECT json_extract(t.document,'$.title'), json_extract(t.document,'$.artists')
 FROM members m JOIN tracks t ON t.id=m.source_id JOIN music_albums a ON a.owner=t.owner AND a.id=?
 WHERE t.owner=? AND (?='' OR t.provider=?) AND ` + ordinaryMusicTrack + ` AND ` + ordinaryMusicAlbumMetadata + `
 AND json_type(t.document,'$.title')='text' AND json_type(t.document,'$.artists')='array' ORDER BY t.id`
	evidence := []music.AlbumTrackEvidence{}
	names := map[string]bool{}
	err := musicRows(ctx, tx, sources, func(r *sql.Rows) error {
		var track music.AlbumTrackEvidence
		var artists []byte
		if err := r.Scan(&track.Title, &artists); err != nil {
			return err
		}
		if err := json.Unmarshal(artists, &track.ArtistNames); err != nil {
			return err
		}
		if strings.TrimSpace(track.Title) == "" {
			return nil
		}
		track.ArtistNames = slices.DeleteFunc(track.ArtistNames, func(name string) bool { return strings.TrimSpace(name) == "" })
		if len(track.ArtistNames) != 0 {
			for _, name := range track.ArtistNames {
				names[name] = true
			}
			evidence = append(evidence, track)
		}
		return nil
	}, owner, id, owner, id, owner, id, owner, provider, provider)
	if err != nil || len(evidence) == 0 {
		return evidence, err
	}
	aliases, err := evidenceArtistAliases(ctx, tx, owner, names)
	if err != nil {
		return nil, err
	}
	for i := range evidence {
		for _, name := range evidence[i].ArtistNames {
			evidence[i].ArtistNames = append(evidence[i].ArtistNames, aliases[name]...)
		}
	}
	return evidence, nil
}

// 只扩展本人曲库中能唯一确定歌手的别名；两个歌手共用的名字不能作为新增证据。
func evidenceArtistAliases(ctx context.Context, tx *sql.Tx, owner string, names map[string]bool) (map[string][]string, error) {
	labels := make([]string, 0, len(names))
	for name := range names {
		labels = append(labels, name)
	}
	raw, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	const query = `WITH requested AS (SELECT value AS label FROM json_each(?)), matches AS (
 SELECT r.label,a.id FROM requested r JOIN music_artists a ON a.owner=? AND a.name=r.label
 UNION SELECT r.label,n.artist_id FROM requested r JOIN music_artist_localized_names n
 ON n.name COLLATE NOCASE=r.label COLLATE NOCASE JOIN music_artists a ON a.id=n.artist_id AND a.owner=?
), artists AS (
 SELECT label,min(id) AS id FROM matches GROUP BY label HAVING count(*)=1
), candidates AS (
 SELECT s.label,s.id,a.name FROM artists s JOIN music_artists a ON a.id=s.id
 UNION SELECT s.label,s.id,n.name FROM artists s JOIN music_artist_localized_names n ON n.artist_id=s.id
)
 SELECT c.label,c.name FROM candidates c WHERE lower(c.label)<>lower(c.name)
 AND NOT EXISTS(SELECT 1 FROM music_artists a WHERE a.owner=? AND a.id<>c.id
  AND (a.name COLLATE NOCASE=c.name COLLATE NOCASE OR a.name COLLATE NOCASE=c.label COLLATE NOCASE))
 AND NOT EXISTS(SELECT 1 FROM music_artist_localized_names n JOIN music_artists a ON a.id=n.artist_id
  WHERE n.name COLLATE NOCASE=c.name COLLATE NOCASE AND a.owner=? AND a.id<>c.id)
 ORDER BY c.label,c.name`
	aliases := map[string][]string{}
	err = musicRows(ctx, tx, query, func(r *sql.Rows) error {
		var label, alias string
		if err := r.Scan(&label, &alias); err != nil {
			return err
		}
		aliases[label] = append(aliases[label], alias)
		return nil
	}, string(raw), owner, owner, owner, owner)
	return aliases, err
}

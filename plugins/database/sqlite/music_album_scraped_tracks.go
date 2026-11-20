package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

type scrapedAlbumMember struct{ id, title, artist string }
type scrapedAlbumRef struct{ track, plugin, release string }

// 只补充曲目和专辑关系，不改已有曲目资料、来源或用户额外关联的成员。
func syncMusicAlbumScrapedTracks(ctx context.Context, tx *sql.Tx, owner, album string, field works.ScrapedField) error {
	members := []scrapedAlbumMember{}
	if err := musicRows(ctx, tx, `SELECT m.id,m.title,coalesce((SELECT group_concat(name,' / ') FROM (
 SELECT a.name FROM music_track_artists c JOIN music_artists a ON a.id=c.artist_id WHERE c.track_id=m.id ORDER BY c.position)), '')
 FROM music_album_tracks x JOIN music_tracks m ON m.id=x.track_id WHERE x.album_id=? AND m.owner=? ORDER BY x.position,m.id`, func(row *sql.Rows) error {
		var m scrapedAlbumMember
		if err := row.Scan(&m.id, &m.title, &m.artist); err != nil {
			return err
		}
		members = append(members, m)
		return nil
	}, album, owner); err != nil {
		return err
	}
	refs := map[string]scrapedAlbumRef{}
	if err := musicRows(ctx, tx, `SELECT r.slot,r.track_id,r.plugin_id,r.release_id FROM music_album_scraped_track_refs r
 JOIN music_tracks m ON m.id=r.track_id AND m.owner=? WHERE r.album_id=?`, func(row *sql.Rows) error {
		var slot string
		var r scrapedAlbumRef
		if err := row.Scan(&slot, &r.track, &r.plugin, &r.release); err != nil {
			return err
		}
		refs[slot] = r
		return nil
	}, owner, album); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM music_album_scraped_track_refs WHERE album_id=?", album); err != nil {
		return err
	}
	used := map[string]bool{}
	slots := map[string]int{}
	for position, track := range field.Item.Tracks {
		title := strings.TrimSpace(track.Title)
		artist := strings.TrimSpace(cmp.Or(track.Artist, field.Item.Artist))
		input, err := (music.TrackInput{TrackFields: music.TrackFields{Title: title}, ArtistNames: scrapedArtistNames(artist)}).Normalize()
		if err != nil {
			return err
		}
		slot := fmt.Sprintf("%d:%d", track.Disc, track.Number)
		if track.Number == 0 {
			slot = fmt.Sprintf("index:%d", position)
		}
		slots[slot]++
		if slots[slot] > 1 {
			slot = fmt.Sprintf("%s:%d", slot, slots[slot])
		}
		id := ""
		if prior := refs[slot]; prior.plugin == field.PluginID && prior.release == field.Item.ID && !used[prior.track] {
			id = prior.track
		}
		if id == "" {
			matches := []scrapedAlbumMember{}
			for _, member := range members {
				if member.title == title && !used[member.id] {
					matches = append(matches, member)
				}
			}
			if len(matches) > 1 && artist != "" {
				filtered := []scrapedAlbumMember{}
				for _, match := range matches {
					if match.artist == artist {
						filtered = append(filtered, match)
					}
				}
				matches = filtered
			}
			if len(matches) == 1 {
				id = matches[0].id
			}
		}
		if id == "" {
			id = domain.NewID()
			if _, err = tx.ExecContext(ctx, "INSERT INTO music_tracks(id,owner,title,origin,metadata_locked) VALUES(?,?,?,'manual',1)", id, owner, input.Title); err != nil {
				return err
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err = tx.ExecContext(ctx, "INSERT INTO recordings(id,owner,created_at,updated_at) VALUES(?,?,?,?)", id, owner, now, now); err != nil {
				return err
			}
			if err = writeMusicCredits(ctx, tx, owner, id, input.ArtistNames); err != nil {
				return err
			}
		}
		used[id] = true
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_manual_album_tracks(album_id,track_id,position) VALUES(?,?,?)
 ON CONFLICT(album_id,track_id) DO UPDATE SET position=excluded.position`, album, id, position); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_album_scraped_track_refs(album_id,slot,track_id,plugin_id,release_id,position)
 VALUES(?,?,?,?,?,?)`, album, slot, id, field.PluginID, field.Item.ID, position); err != nil {
			return err
		}
	}
	return nil
}

func scrapedArtistNames(value string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, name := range strings.Split(value, "/") {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result
}

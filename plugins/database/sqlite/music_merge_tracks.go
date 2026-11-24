package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

// MergeMusicAlbumTracks 在同一事务内按用户选定的主曲目合并专辑成员、来源与资料。
func (s *Store) MergeMusicAlbumTracks(ctx context.Context, owner, albumID string, in music.MergeTracksInput, key string) (music.Track, error) {
	if err := in.Validate(); err != nil {
		return music.Track{}, err
	}
	if albumID == "" {
		return music.Track{}, domain.Fail("INVALID_ARGUMENT", "Album is required")
	}
	if in.Metadata != nil {
		metadata, err := in.Metadata.Normalize()
		if err != nil {
			return music.Track{}, err
		}
		in.Metadata = &metadata
	}
	return saveMusic(ctx, s, owner, "albums/merge-tracks", albumID, key, in.ExpectedAlbumRevision, in, func(tx *sql.Tx, id string) (music.Track, error) {
		album, err := readMusicAlbum(ctx, tx, owner, id)
		if err != nil {
			return music.Track{}, err
		}
		revision, err := matchingRevision(ctx, tx, owner)
		if err != nil {
			return music.Track{}, err
		}
		if album.Revision != in.ExpectedAlbumRevision || revision != in.ExpectedMatchingRevision {
			return music.Track{}, domain.Fail("CONFLICT", "Album or source grouping changed")
		}
		ids := make([]string, 0, len(in.Tracks))
		for _, selection := range in.Tracks {
			ids = append(ids, selection.TrackID)
		}
		rawIDs, _ := json.Marshal(ids)
		var visible int
		err = tx.QueryRowContext(ctx, musicVisible+`SELECT count(*) FROM visible_music_tracks v JOIN music_album_tracks a ON a.track_id=v.id
 WHERE a.album_id=? AND a.track_id IN(SELECT value FROM json_each(?))`, owner, owner, owner, id, string(rawIDs)).Scan(&visible)
		if err != nil {
			return music.Track{}, err
		}
		if visible != len(ids) {
			return music.Track{}, domain.Fail("CONFLICT", "Track is no longer visible in the selected album")
		}
		var target music.Track
		var donors []music.Track
		for _, selection := range in.Tracks {
			track, err := readMusicTrack(ctx, tx, owner, selection.TrackID)
			if err != nil {
				return music.Track{}, err
			}
			if track.Revision != selection.ExpectedRevision {
				return music.Track{}, domain.Fail("CONFLICT", "Track metadata changed")
			}
			if track.ID == in.TargetTrackID {
				target = track
				continue
			}
			donors = append(donors, track)
		}
		group, err := prepareAlbumMergeRecording(ctx, tx, owner, target, donors)
		if err != nil {
			return music.Track{}, err
		}
		if group.id != target.ID {
			donors = append([]music.Track{target}, donors...)
			target.ID = group.id
		}
		// 加锁后来源触发器不会用其中一个文件的标签覆盖主曲目的署名或标题。
		if _, err := tx.ExecContext(ctx, "UPDATE music_tracks SET metadata_locked=1 WHERE id=? AND owner=?", target.ID, owner); err != nil {
			return music.Track{}, err
		}
		if err := rememberAlbumMergeSlots(ctx, tx, id, target.ID, in); err != nil {
			return music.Track{}, err
		}
		// 将受影响的其它专辑也纳入修订范围，它们的成员会在同一事务改指主曲目。
		albumIDs := map[string]bool{id: true}
		for _, a := range target.Albums {
			albumIDs[a.ID] = true
		}
		for _, donor := range donors {
			for _, a := range donor.Albums {
				albumIDs[a.ID] = true
			}
			if in.Metadata == nil {
				if err := mergeTrackTitles(&target, donor); err != nil {
					return music.Track{}, err
				}
			}
			if err := moveTrackRelations(ctx, tx, owner, target.ID, donor.ID); err != nil {
				return music.Track{}, err
			}
		}
		if err := group.apply(ctx, tx, owner, in.ExpectedMatchingRevision); err != nil {
			return music.Track{}, err
		}
		for _, donor := range donors {
			if err := retireMergedTrack(ctx, tx, owner, donor); err != nil {
				return music.Track{}, err
			}
		}
		if in.Metadata != nil {
			target.TrackFields = in.Metadata.TrackFields
			if err := writeMusicCredits(ctx, tx, owner, target.ID, in.Metadata.ArtistNames); err != nil {
				return music.Track{}, err
			}
		}
		raw, err := json.Marshal(target.LocalizedTitles)
		if err != nil {
			return music.Track{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_tracks SET title=?,localized_titles=?,cover_path=?,genre=?,year=?,rating=?,
 metadata_locked=1,scraper_placeholder=0,revision=revision+1 WHERE owner=? AND id=?`, target.Title, string(raw), target.CoverPath, target.Genre, target.Year, target.Rating, owner, target.ID); err != nil {
			return music.Track{}, err
		}
		for album := range albumIDs {
			if _, err := tx.ExecContext(ctx, "UPDATE music_albums SET revision=revision+1 WHERE owner=? AND id=?", owner, album); err != nil {
				return music.Track{}, err
			}
		}
		return readMusicTrack(ctx, tx, owner, target.ID)
	})
}

func mergeTrackTitles(target *music.Track, donor music.Track) error {
	aliases, err := mergedScrapedTitles(target.Title, target.LocalizedTitles, music.TrackInput{TrackFields: donor.TrackFields})
	if err != nil {
		return err
	}
	if aliases == nil {
		aliases = []scraper.LocalizedTitle{}
	}
	target.LocalizedTitles = aliases
	if target.CoverPath == nil {
		target.CoverPath = donor.CoverPath
	}
	if target.Genre == nil {
		target.Genre = donor.Genre
	}
	if target.Year == nil {
		target.Year = donor.Year
	}
	if target.Rating == nil {
		target.Rating = donor.Rating
	}
	return nil
}

// 当前专辑的曲序已经由用户裁决，遇到其它专辑的双曲位时保留原状并返回冲突。
func moveTrackRelations(ctx context.Context, tx *sql.Tx, owner, target, donor string) error {
	var conflict bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_album_scraped_track_refs d
 JOIN music_album_scraped_track_refs t ON t.album_id=d.album_id WHERE t.track_id=? AND d.track_id=?)`, target, donor).Scan(&conflict)
	if err != nil {
		return err
	}
	if conflict {
		return domain.Fail("ALBUM_TRACK_SLOT_CONFLICT", "The selected tracks occupy different positions in another album")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO music_manual_album_tracks(album_id,track_id,position)
 SELECT album_id,?,position FROM music_manual_album_tracks WHERE track_id=?
 ON CONFLICT(album_id,track_id) DO UPDATE SET position=min(music_manual_album_tracks.position,excluded.position)`, target, donor)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE music_album_scraped_track_refs SET track_id=? WHERE track_id=?", target, donor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE music_album_merged_slots SET track_id=? WHERE track_id=?", target, donor); err != nil {
		return err
	}
	// 追加现有歌手身份并保持主曲目的原署名顺序。
	_, err = tx.ExecContext(ctx, `INSERT INTO music_track_artists(track_id,artist_id,position)
 SELECT ?,artist_id,(SELECT coalesce(max(position),-1) FROM music_track_artists WHERE track_id=?)+row_number() OVER(ORDER BY position)
 FROM music_track_artists d WHERE track_id=? AND NOT EXISTS(SELECT 1 FROM music_track_artists t WHERE t.track_id=? AND t.artist_id=d.artist_id)`, target, target, donor, target)
	if err != nil {
		return err
	}
	if err = moveTrackPlaylistEntries(ctx, tx, owner, target, donor); err != nil {
		return err
	}
	return nil
}

func retireMergedTrack(ctx context.Context, tx *sql.Tx, owner string, donor music.Track) error {
	// 源曲目 ID 仍归该文件，解除关联时恢复资料；移除已转交的专辑关联，避免残留重复行。
	if len(donor.Sources) == 1 && donor.Sources[0].SourceTrackID == donor.ID {
		_, err := tx.ExecContext(ctx, "DELETE FROM music_manual_album_tracks WHERE track_id=?", donor.ID)
		return err
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM music_tracks WHERE owner=? AND id=?", owner, donor.ID)
	if err != nil {
		return err
	}
	return deleteUnusedMusicRecording(ctx, tx, owner, donor.ID)
}

func deleteUnusedMusicRecording(ctx context.Context, tx *sql.Tx, owner, id string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM recordings WHERE id=? AND owner=?
 AND NOT EXISTS(SELECT 1 FROM recording_source_links WHERE recording_id=recordings.id)
 AND NOT EXISTS(SELECT 1 FROM recording_default_sources WHERE recording_id=recordings.id)
 AND NOT EXISTS(SELECT 1 FROM recording_cover_preferences WHERE recording_id=recordings.id)`, id, owner)
	return err
}

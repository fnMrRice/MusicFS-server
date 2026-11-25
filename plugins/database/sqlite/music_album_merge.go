package sqlite

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

var musicAlbumMergeFields = []string{"name", "artist", "releaseDate", "publisher", "genre", "description", "cover", "tracks"}

// MergeMusicAlbumMetadata 按人工勾选的字段合并，未选资料和曲序保持原样。
func (s *Store) MergeMusicAlbumMetadata(ctx context.Context, owner, id string, revision int64, candidates []works.ScrapedField, selected []string, key string) (int64, error) {
	if !works.ValidID(id) || len(candidates) != 2 || len(selected) == 0 || len(selected) > len(musicAlbumMergeFields) {
		return 0, domain.Fail("INVALID_ARGUMENT", "Album candidates and selected merge fields are required")
	}
	candidates = slices.Clone(candidates)
	slices.SortFunc(candidates, func(a, b works.ScrapedField) int { return strings.Compare(a.Field, b.Field) })
	if candidates[0].Field != "albumDetails" || candidates[1].Field != "albumTracks" {
		return 0, domain.Fail("INVALID_ARGUMENT", "Album details and complete tracks are required")
	}
	for _, candidate := range candidates {
		if !works.ValidID(candidate.PluginID) || !works.ValidID(candidate.CandidateToken) || candidate.PluginID != candidates[0].PluginID {
			return 0, domain.Fail("INVALID_ARGUMENT", "Invalid album merge candidates")
		}
	}
	selected = slices.Clone(selected)
	slices.Sort(selected)
	for i, field := range selected {
		if !slices.Contains(musicAlbumMergeFields, field) || i > 0 && selected[i-1] == field {
			return 0, domain.Fail("INVALID_ARGUMENT", "Invalid selected album merge field")
		}
	}
	input := struct {
		PluginID, Details, Tracks string
		Fields                    []string
	}{candidates[0].PluginID, candidates[0].CandidateToken, candidates[1].CandidateToken, selected}
	return saveMusic(ctx, s, owner, "albums/metadata-merge", id, key, revision, input, func(tx *sql.Tx, albumID string) (int64, error) {
		album, err := readMusicAlbum(ctx, tx, owner, albumID)
		if err != nil {
			return 0, err
		}
		if album.Revision != revision {
			return 0, domain.Fail("CONFLICT", "Music album revision changed")
		}
		details, tracks := candidates[0], candidates[1]
		if details.Item.ID == "" || tracks.Item.ID == "" {
			return 0, domain.Fail("NOT_FOUND", "Album merge candidate expired or unavailable")
		}
		if details.Item.ID != tracks.Item.ID || len(tracks.Item.Tracks) == 0 {
			return 0, domain.Fail("INVALID_ARGUMENT", "Album candidates must describe the same release and complete track list")
		}
		if err = setMusicAlbumSource(ctx, tx, album, details, false); err != nil {
			return 0, err
		}
		current := works.ScrapedField{Field: "albumDetails", PluginID: details.PluginID}
		var raw []byte
		err = tx.QueryRowContext(ctx, "SELECT document FROM music_album_scraped_fields WHERE album_id=? AND field='albumDetails'", albumID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if err == nil {
			if err = json.Unmarshal(raw, &current); err != nil {
				return 0, err
			}
		}
		if current.Item.Title == "" {
			current.Item.Title = album.Name
		}
		current.PluginID, current.Item.ID, current.Item.SourceURL = details.PluginID, details.Item.ID, details.Item.SourceURL
		current.CandidateToken = ""
		for _, field := range selected {
			switch field {
			case "name":
				album.Name = strings.TrimSpace(cmp.Or(details.Item.Album, details.Item.Title))
				current.Item.Title = album.Name
				current.Item.Album = details.Item.Album
			case "artist":
				current.Item.Artist = details.Item.Artist
			case "genre":
				current.Item.Genre = details.Item.Genre
			case "description":
				current.Item.Description = details.Item.Description
			case "publisher":
				current.Item.Publisher = details.Item.Publisher
				album.Publisher = albumOptional(details.Item.Publisher)
			case "releaseDate":
				current.Item.Date = details.Item.Date
				album.ReleaseDate, err = albumMergedDate(details.Item.Date)
				if err != nil {
					return 0, err
				}
			case "cover":
				image := cmp.Or(details.Item.ImageURL, tracks.Item.ImageURL)
				if image != "" && !usableAlbumCoverPath(image) {
					return 0, domain.Fail("INVALID_ARGUMENT", "Incoming album artwork URL is invalid")
				}
				current.Item.ImageURL = image
				album.CoverPath = albumOptional(image)
				if _, err = tx.ExecContext(ctx, "DELETE FROM music_album_scraped_fields WHERE album_id=? AND field='artwork'", albumID); err != nil {
					return 0, err
				}
				if image != "" {
					art := details
					art.Field = "artwork"
					art.CandidateToken = ""
					art.Item.ImageURL = image
					art.Item.Tracks = nil
					if err = writeMusicAlbumScrapedField(ctx, tx, albumID, art); err != nil {
						return 0, err
					}
				}
			case "tracks":
				if err = syncMusicAlbumScrapedTracks(ctx, tx, owner, albumID, tracks, true); err != nil {
					return 0, err
				}
				tracks.CandidateToken = ""
				if err = writeMusicAlbumScrapedField(ctx, tx, albumID, tracks); err != nil {
					return 0, err
				}
			}
		}
		if _, err = album.AlbumFields.Normalize(); err != nil {
			return 0, err
		}
		if err = writeMusicAlbumScrapedField(ctx, tx, albumID, current); err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, "UPDATE music_albums SET name=?,publisher=?,release_date=?,cover_path=?,revision=revision+1,metadata_locked=1 WHERE owner=? AND id=? AND revision=?", album.Name, album.Publisher, album.ReleaseDate, album.CoverPath, owner, albumID, revision)
		if err != nil {
			return 0, err
		}
		if err = checkMusicUpdate(ctx, tx, result, owner, albumID, "music_albums"); err != nil {
			return 0, err
		}
		return revision + 1, nil
	})
}

func albumOptional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func albumMergedDate(value string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	format := time.DateOnly
	if len(value) == 4 {
		format = "2006"
	} else if len(value) == 7 {
		format = "2006-01"
	} else if len(value) > 10 {
		value = value[:10]
	}
	if _, err := time.Parse(format, value); err != nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Incoming album release date is invalid")
	}
	if format != time.DateOnly {
		return nil, nil
	}
	return &value, nil
}

func writeMusicAlbumScrapedField(ctx context.Context, tx *sql.Tx, album string, field works.ScrapedField) error {
	raw, err := json.Marshal(field)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO music_album_scraped_fields(album_id,field,document) VALUES(?,?,?)
 ON CONFLICT(album_id,field) DO UPDATE SET document=excluded.document`, album, field.Field, string(raw))
	return err
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func musicRows(ctx context.Context, tx *sql.Tx, query string, scan func(*sql.Rows) error, args ...any) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err = scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

const albumColumns = "a.id,a.name,a.external_id,a.is_external,a.cover_path,a.publisher,a.release_date,a.source_links,a.original_name,a.revision"

type albumLinksColumn struct{ links *[]music.AlbumSourceLink }

func (column albumLinksColumn) Scan(value any) error {
	raw, ok := value.(string)
	if !ok {
		return domain.Fail("INVALID_RESPONSE", "Invalid stored album links")
	}
	return json.Unmarshal([]byte(raw), column.links)
}

func albumFields(a *music.Album) []any {
	return []any{&a.ID, &a.Name, &a.ExternalID, &a.IsExternal, &a.CoverPath, &a.Publisher, &a.ReleaseDate, albumLinksColumn{&a.SourceLinks}, &a.OriginalName, &a.Revision}
}

func loadMusicRelations(ctx context.Context, tx *sql.Tx, owner string, items []music.Track, ordinary bool, scope albumContext) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	byID := make(map[string]*music.Track, len(items))
	for i := range items {
		ids[i] = items[i].ID
		byID[ids[i]] = &items[i]
	}
	raw, _ := json.Marshal(ids)
	err := musicRows(ctx, tx, `SELECT x.track_id,`+artistNameColumns+` FROM music_track_artists x JOIN music_artists a ON a.id=x.artist_id
 WHERE a.owner=? AND x.track_id IN(SELECT value FROM json_each(?)) ORDER BY x.track_id,x.position,a.id`, func(r *sql.Rows) error {
		var id string
		var a music.Artist
		if err := scanArtistNames(r, &a, &id); err != nil {
			return err
		}
		byID[id].Artists = append(byID[id].Artists, a)
		return nil
	}, owner, string(raw))
	if err != nil {
		return err
	}
	albumFilter := ""
	if ordinary {
		albumFilter = " AND " + ordinaryMusicAlbumMetadata
	}
	tagProviders := map[string]string{}
	err = musicRows(ctx, tx, `SELECT x.track_id,`+passiveAlbumProvider+`,`+albumColumns+` FROM music_album_tracks x JOIN music_albums a ON a.id=x.album_id
 WHERE a.owner=? AND x.track_id IN(SELECT value FROM json_each(?))`+albumFilter+` ORDER BY x.track_id,a.name COLLATE NOCASE,a.id`, func(r *sql.Rows) error {
		var id, tagProvider string
		var a music.Album
		if err := r.Scan(append([]any{&id, &tagProvider}, albumFields(&a)...)...); err != nil {
			return err
		}
		byID[id].Albums = append(byID[id].Albums, a)
		byID[id].AlbumIDs = append(byID[id].AlbumIDs, a.ID)
		tagProviders[a.ID] = tagProvider
		return nil
	}, owner, string(raw))
	if err != nil {
		return err
	}
	albumCovers := []*music.Album{}
	for i := range items {
		track := &items[i]
		track.Albums = displayAlbums(track.Albums, scope, func(a music.Album) albumCredit {
			return albumCredit{a.ID, a.Name, tagProviders[a.ID]}
		})
		for j := range items[i].Albums {
			albumCovers = append(albumCovers, &items[i].Albums[j])
		}
	}
	if err = populateMusicAlbumCovers(ctx, tx, owner, albumCovers...); err != nil {
		return err
	}
	prefix, where := "", ""
	args := []any{owner, string(raw)}
	if ordinary {
		prefix = ordinaryMusicSources
		where = " AND " + ordinaryMusicTrack
		args = append([]any{owner}, args...)
	}
	type group struct {
		members []library.Track
		manual  *string
	}
	groups := map[string]*group{}
	err = musicRows(ctx, tx, prefix+`SELECT s.track_id,s.id,s.provider_instance_id,s.plugin_id,s.external_id,s.created_at,s.updated_at,t.asset_id,t.document,l.recording_id,d.source_id
 FROM music_sources s JOIN tracks t ON t.id=s.id AND t.owner=s.owner
 LEFT JOIN recording_source_links l ON l.owner=s.owner AND l.source_id=s.id
 LEFT JOIN recording_default_sources d ON d.owner=l.owner AND d.recording_id=l.recording_id
 WHERE s.owner=? AND s.track_id IN(SELECT value FROM json_each(?))`+where+` ORDER BY s.track_id,s.id`, func(r *sql.Rows) error {
		var id string
		var source music.Source
		var recording, manual *string
		var asset string
		var document []byte
		if err := r.Scan(&id, &source.SourceTrackID, &source.InstanceID, &source.PluginID, &source.SourceID, &source.CreatedAt, &source.UpdatedAt, &asset, &document, &recording, &manual); err != nil {
			return err
		}
		var member library.Track
		if err := json.Unmarshal(document, &member); err != nil || member.ID != source.SourceTrackID || member.ProviderID != source.InstanceID {
			member = library.Track{ID: source.SourceTrackID, ProviderID: source.InstanceID, AssetID: asset, DisplayName: byID[id].Title, Availability: "unavailable"}
		}
		member.RecordingID, member.ManualSourceTrackID, member.Versions = recording, nil, nil
		g := groups[id]
		if g == nil {
			g = &group{manual: manual}
			groups[id] = g
		}
		if len(g.members) >= matching.MaxSources {
			return domain.Fail("RATE_LIMITED", "Recording member limit exceeded")
		}
		g.members = append(g.members, member)
		byID[id].Sources = append(byID[id].Sources, source)
		return nil
	}, args...)
	if err != nil {
		return err
	}
	covers := make([]*library.Track, 0, len(groups))
	for id, g := range groups {
		selected := representativeTrack(g.members, g.manual)
		byID[id].SelectedSource = &selected
		covers = append(covers, &selected)
	}
	if err = populateTrackCovers(ctx, tx, owner, covers...); err != nil {
		return err
	}
	for i := range items {
		track := &items[i]
		if track.SelectedSource == nil {
			continue
		}
		members := map[string]library.Track{}
		for _, v := range track.SelectedSource.Versions {
			members[v.ID] = v
		}
		for j := range track.Sources {
			source := &track.Sources[j]
			member := members[source.SourceTrackID]
			source.Downloaded, source.DownloadVersions = member.Downloaded, member.DownloadVersions
		}
	}
	return nil
}

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/catalog"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

type catalogTrack struct {
	catalog.Track
	directories []catalog.Directory
	albumIDs    []string
	albumNames  []string
}
type catalogData struct {
	tracks   []catalogTrack
	albums   []catalog.Album
	members  map[string][]string
	revision string
}

func catalogID(owner, provider, kind, value string) string {
	b, _ := json.Marshal([]string{owner, provider, kind, value})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:24])
}
func artistID(owner, provider, name string) string {
	return catalogID(owner, provider, "artist-credit", name)
}
func directoryID(owner, provider, root, relative string) string {
	return catalogID(owner, provider, "directory:"+root, relative)
}

func catalogDirectories(owner, provider, root, filename string) []catalog.Directory {
	directory := path.Dir(strings.ReplaceAll(filename, "\\", "/"))
	result := []catalog.Directory{}
	for {
		id := directoryID(owner, provider, root, directory)
		item := catalog.Directory{ID: id, ProviderInstanceID: provider, RootID: root, Name: path.Base(directory)}
		if directory == "." || directory == "/" {
			item.Name = "Root"
			result = append(result, item)
			break
		}
		parent := directoryID(owner, provider, root, path.Dir(directory))
		item.ParentID = &parent
		result = append(result, item)
		next := path.Dir(directory)
		if next == directory {
			break
		}
		directory = next
	}
	return result
}

func readCatalogData(ctx context.Context, tx *sql.Tx, owner, provider string) (catalogData, error) {
	data := catalogData{tracks: []catalogTrack{}, albums: []catalog.Album{}, members: map[string][]string{}}
	rows, err := tx.QueryContext(ctx, `SELECT t.document,t.root_id,t.path,EXISTS(SELECT 1 FROM personal_favorites f WHERE f.owner=t.owner AND f.source_track_id=t.id) FROM tracks t WHERE t.owner=? AND (?='' OR t.provider=?) ORDER BY t.id LIMIT ?`, owner, provider, provider, catalog.MaxItems+1)
	if err != nil {
		return data, err
	}
	bytes := 0
	for rows.Next() {
		var raw []byte
		var root, filename string
		var item catalogTrack
		if err = rows.Scan(&raw, &root, &filename, &item.Favorite); err != nil {
			rows.Close()
			return data, err
		}
		bytes += len(raw)
		if len(data.tracks) == catalog.MaxItems || bytes > catalog.MaxSnapshotBytes {
			rows.Close()
			return data, domain.Fail("RATE_LIMITED", "Catalog query exceeds its resource budget")
		}
		if err = json.Unmarshal(raw, &item.Track.Track); err != nil {
			rows.Close()
			return data, err
		}
		item.RootID = root
		item.Source = playlists.SourceRef{SourceTrackID: &item.ID}
		item.directories = catalogDirectories(owner, item.ProviderID, root, filename)
		item.DirectoryID = item.directories[0].ID
		data.tracks = append(data.tracks, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT a.id,a.provider_id,a.name,a.revision,(SELECT b.object_id FROM album_cover_heads h JOIN album_cover_bindings b ON b.id=h.binding_id JOIN media_content_objects o ON o.id=b.object_id JOIN tracks t ON t.id=b.source_id AND t.owner=b.owner AND t.asset_id=b.asset_id AND t.version=b.asset_version JOIN source_assets v ON v.id=b.asset_id AND v.owner=b.owner AND v.current=1 WHERE h.owner=a.owner AND h.target_kind='sourceAlbum' AND h.target_id=a.id AND b.target_revision=a.revision AND o.state='available') FROM source_albums a WHERE a.owner=? AND (?='' OR a.provider_id=?) ORDER BY a.id LIMIT ?`, owner, provider, provider, catalog.MaxItems+1)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var a catalog.Album
		a.Artists = []string{}
		if err = rows.Scan(&a.ID, &a.ProviderInstanceID, &a.Name, &a.Revision, &a.CoverObjectID); err != nil {
			rows.Close()
			return data, err
		}
		if len(data.albums) == catalog.MaxItems {
			rows.Close()
			return data, domain.Fail("RATE_LIMITED", "Catalog album budget exceeded")
		}
		data.albums = append(data.albums, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT m.album_id,m.source_id FROM source_album_members m JOIN source_albums a ON a.id=m.album_id JOIN tracks t ON t.id=m.source_id AND t.owner=a.owner AND t.provider=a.provider_id WHERE a.owner=? AND (?='' OR a.provider_id=?) ORDER BY m.album_id,m.position`, owner, provider, provider)
	if err != nil {
		return data, err
	}
	membershipCount := 0
	for rows.Next() {
		var album, track string
		if err = rows.Scan(&album, &track); err != nil {
			rows.Close()
			return data, err
		}
		data.members[album] = append(data.members[album], track)
		membershipCount++
		if membershipCount > catalog.MaxItems*4 {
			rows.Close()
			return data, domain.Fail("RATE_LIMITED", "Catalog membership budget exceeded")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return data, err
	}
	indices := map[string]int{}
	for i, t := range data.tracks {
		indices[t.ID] = i
	}
	for i := range data.albums {
		a := &data.albums[i]
		for _, id := range data.members[a.ID] {
			index, ok := indices[id]
			if !ok {
				continue
			}
			t := &data.tracks[index]
			t.albumIDs = append(t.albumIDs, a.ID)
			t.albumNames = append(t.albumNames, a.Name)
			a.Artists = append(a.Artists, t.Artists...)
			a.TrackCount++
		}
		slices.Sort(a.Artists)
		a.Artists = slices.Compact(a.Artists)
	}
	var libraryRevision, favoriteRevision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&libraryRevision); err != nil {
		return data, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT revision FROM personal_collection_revisions WHERE owner=? AND kind='favorites'),0)", owner).Scan(&favoriteRevision); err != nil {
		return data, err
	}
	state, _ := json.Marshal(data.albums)
	digest := sha256.Sum256(append([]byte(fmt.Sprintf("%d:%d:", libraryRevision, favoriteRevision)), state...))
	data.revision = hex.EncodeToString(digest[:])
	return data, nil
}

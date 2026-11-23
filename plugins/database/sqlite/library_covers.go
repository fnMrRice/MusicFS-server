package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

type trackCoverQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const currentTrackCoverJoins = `
 JOIN source_assets a ON a.owner=t.owner AND a.id=t.asset_id AND a.track_id=t.id AND a.version=t.version AND a.current=1
 JOIN media_content_slots s ON s.owner=t.owner AND s.source_id=t.id AND s.field='artwork' AND s.language=''
 JOIN media_content_bindings b ON b.id=s.binding_id AND b.owner=s.owner AND b.source_id=t.id AND b.asset_id=t.asset_id AND b.field=s.field AND b.language=s.language AND b.current=1
 AND json_extract(b.document,'$.target.sourceTrackId')=t.id
 AND json_extract(b.document,'$.target.assetId')=t.asset_id
 AND json_extract(b.document,'$.target.assetVersion')=t.version
 JOIN media_content_objects o ON o.id=b.object_id AND o.owner=b.owner AND o.source_id=t.id AND o.state='available'`

// populateTrackCovers 批量补充当前封面，不把历史分页快照中的旧资产封面当成当前选择。
// 媒体对象可在写回后复用，因此有效版本以绑定和当前资产为准，不要求对象的原始资产相同。
func populateTrackCovers(ctx context.Context, query trackCoverQuery, owner string, tracks ...*library.Track) error {
	byID := map[string][]*library.Track{}
	var collect func(*library.Track)
	collect = func(track *library.Track) {
		track.CoverObjectID = nil
		track.Downloaded, track.DownloadVersions = false, nil
		byID[track.ID] = append(byID[track.ID], track)
		for i := range track.Versions {
			collect(&track.Versions[i])
		}
	}
	for _, track := range tracks {
		collect(track)
	}
	if len(byID) == 0 {
		return nil
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err = readDownloadVersions(ctx, query, owner, string(raw), func(id string, v onlinecache.Version) {
		for _, track := range byID[id] {
			track.Downloaded = true
			track.DownloadVersions = append(track.DownloadVersions, v)
		}
	}); err != nil {
		return err
	}
	rows, err := query.QueryContext(ctx, `SELECT t.id,t.asset_id,t.version,b.object_id FROM tracks t`+currentTrackCoverJoins+`
 WHERE t.owner=? AND t.id IN (SELECT value FROM json_each(?))`, owner, string(raw))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, asset, version, object string
		if err = rows.Scan(&id, &asset, &version, &object); err != nil {
			rows.Close()
			return err
		}
		for _, track := range byID[id] {
			if track.AssetID == asset && track.Availability == "available" && (track.Asset == nil || track.Asset.Version == version) {
				track.CoverObjectID = &object
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return populateRecordingTrackCovers(ctx, query, owner, tracks)
}

func populatePageTrackCovers(ctx context.Context, query trackCoverQuery, owner string, page *library.Page) error {
	tracks := make([]*library.Track, len(page.Items))
	for i := range page.Items {
		tracks[i] = &page.Items[i]
	}
	return populateTrackCovers(ctx, query, owner, tracks...)
}

func populateCatalogTrackCovers(ctx context.Context, query trackCoverQuery, owner string, page *catalog.Page) error {
	if page.Query.Type != "tracks" {
		return nil
	}
	items := make([]catalog.Track, len(page.Items))
	tracks := make([]*library.Track, len(items))
	for i, raw := range page.Items {
		if err := json.Unmarshal(raw, &items[i]); err != nil {
			return err
		}
		tracks[i] = &items[i].Track
	}
	if err := populateTrackCovers(ctx, query, owner, tracks...); err != nil {
		return err
	}
	for i, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		page.Items[i] = raw
	}
	return nil
}

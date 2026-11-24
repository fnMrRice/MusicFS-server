package sqlite

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

// 在线来源的原始地址留在服务端；含有效本地封面的录音直接采用原选择。
func populateOnlineCoverPaths(ctx context.Context, query trackCoverQuery, owner string, byID map[string][]*library.Track, tracks []*library.Track) error {
	var ids []string
	for id, list := range byID {
		missing := false
		for _, t := range list {
			t.OnlineCoverPath = ""
			missing = missing || t.CoverObjectID == nil && t.Availability == "available"
		}
		if missing {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	rows, err := query.QueryContext(ctx, `SELECT s.id,t.asset_id,n.id FROM music_sources s JOIN tracks t ON s.id=t.id AND s.owner=t.owner
		JOIN music_sources n ON n.owner=s.owner AND n.track_id=s.track_id JOIN tracks a ON a.owner=n.owner AND a.id=n.id
		WHERE s.owner=? AND s.id IN (SELECT value FROM json_each(?)) AND n.plugin_id=? AND a.root_id='online-audio'
		AND json_extract(a.document,'$.availability')='available' AND coalesce(json_extract(a.document,'$._providerCoverUrl'),'legacy')<>''
		ORDER BY s.id,n.id`, owner, string(raw), pluginproto.OnlinePluginID(online.Netease))
	if err != nil {
		return err
	}
	defer rows.Close()
	groupPath := map[string]string{}
	assets := map[string]string{}
	for rows.Next() {
		var id, asset, source string
		if err = rows.Scan(&id, &asset, &source); err != nil {
			return err
		}
		path := "/api/v1/source-tracks/" + url.PathEscape(source) + "/online-artwork"
		assets[id] = asset
		if groupPath[id] == "" {
			groupPath[id] = path
		}
		if id == source {
			for _, t := range byID[id] {
				if t.CoverObjectID == nil && t.AssetID == asset && t.Availability == "available" {
					t.OnlineCoverPath = path
				}
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, t := range tracks {
		if t.CoverObjectID != nil || t.OnlineCoverPath != "" || t.Availability != "available" || t.AssetID != assets[t.ID] {
			continue
		}
		if len(t.Versions) == 0 {
			t.OnlineCoverPath = groupPath[t.ID]
			continue
		}
		for _, version := range t.Versions {
			if version.OnlineCoverPath != "" {
				t.OnlineCoverPath = version.OnlineCoverPath
				break
			}
		}
	}
	return nil
}

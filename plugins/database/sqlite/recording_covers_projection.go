package sqlite

import (
	"context"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library"
)

type recordingCoverSource struct{ recording, asset, version string }

// 录音投影只覆盖顶层曲目，版本列表始终保留各自来源封面。
// 分组快照仅允许当时已有、且现在仍属于该录音及原资产版本的成员提供封面。
func populateRecordingTrackCovers(ctx context.Context, query trackCoverQuery, owner string, tracks []*library.Track) error {
	recordings := map[string]bool{}
	sources := []string{}
	for _, track := range tracks {
		if len(track.Versions) > 0 {
			if track.RecordingID != nil {
				recordings[*track.RecordingID] = true
			}
		} else {
			sources = append(sources, track.ID)
		}
	}
	current := map[string]recordingCoverSource{}
	if len(sources) > 0 {
		raw, err := json.Marshal(sources)
		if err != nil {
			return err
		}
		rows, err := query.QueryContext(ctx, `SELECT t.id,t.asset_id,t.version,COALESCE(l.recording_id,'') FROM tracks t
 LEFT JOIN recording_source_links l ON l.owner=t.owner AND l.source_id=t.id
 WHERE t.owner=? AND t.id IN (SELECT value FROM json_each(?))`, owner, string(raw))
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var source recordingCoverSource
			if err = rows.Scan(&id, &source.asset, &source.version, &source.recording); err != nil {
				rows.Close()
				return err
			}
			current[id] = source
			if source.recording != "" {
				recordings[source.recording] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	ids := make([]string, 0, len(recordings))
	for id := range recordings {
		ids = append(ids, id)
	}
	states, err := readRecordingCoverStates(ctx, query, owner, ids)
	if err != nil {
		return err
	}
	for _, track := range tracks {
		if len(track.Versions) == 0 {
			track.RecordingID = nil
			source, ok := current[track.ID]
			if !ok || source.recording == "" {
				continue
			}
			track.RecordingID = &source.recording
			if track.AssetID != source.asset || track.Asset != nil && track.Asset.Version != source.version {
				track.CoverObjectID = nil
				continue
			}
			track.CoverObjectID = states[source.recording].selected(nil)
			continue
		}
		if track.RecordingID == nil {
			continue
		}
		members := map[string]library.Track{}
		for _, member := range track.Versions {
			members[member.ID] = member
		}
		track.CoverObjectID = states[*track.RecordingID].selected(func(candidate recordingCoverCandidate) bool {
			member, ok := members[candidate.SourceTrackID]
			return ok && member.AssetID == candidate.asset && (member.Asset == nil || member.Asset.Version == candidate.version)
		})
	}
	return nil
}

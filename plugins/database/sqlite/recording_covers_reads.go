package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

type recordingCoverCandidate struct {
	library.RecordingCoverCandidate
	asset, version string
}

type recordingCoverState struct {
	revision   int64
	manual     *string
	candidates []recordingCoverCandidate
}

func recordingCoverLess(left, right recordingCoverCandidate) bool {
	larea, rarea := int64(left.Width)*int64(left.Height), int64(right.Width)*int64(right.Height)
	if larea != rarea {
		return larea > rarea
	}
	if lside, rside := min(left.Width, left.Height), min(right.Width, right.Height); lside != rside {
		return lside > rside
	}
	return left.ObjectID < right.ObjectID
}

func (state recordingCoverState) selected(accept func(recordingCoverCandidate) bool) *string {
	var selected *string
	for _, candidate := range state.candidates {
		if accept != nil && !accept(candidate) {
			continue
		}
		if state.manual != nil && candidate.ObjectID == *state.manual {
			return &candidate.ObjectID
		}
		if selected == nil {
			selected = &candidate.ObjectID
		}
	}
	return selected
}

func (state recordingCoverState) view(id string) library.RecordingCover {
	result := library.RecordingCover{RecordingID: id, Revision: state.revision, ManualObjectID: state.manual, CoverObjectID: state.selected(nil), Candidates: []library.RecordingCoverCandidate{}}
	for _, candidate := range state.candidates {
		result.Candidates = append(result.Candidates, candidate.RecordingCoverCandidate)
	}
	return result
}

// 批量读取录音成员自己的当前封面，不递归读取录音投影，也不为偏好增加媒体保活引用。
func readRecordingCoverStates(ctx context.Context, query trackCoverQuery, owner string, ids []string) (map[string]recordingCoverState, error) {
	result := map[string]recordingCoverState{}
	if len(ids) == 0 {
		return result, nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	// 先按本页录音定位成员，再查成员封面，避免遍历账号的全部封面绑定。
	rows, err := query.QueryContext(ctx, `SELECT r.id,COALESCE(p.revision,0),p.object_id,
 c.object_id,c.source_id,c.provider,c.asset_id,c.version,c.width,c.height
 FROM recordings r LEFT JOIN recording_cover_preferences p ON p.owner=r.owner AND p.recording_id=r.id
 LEFT JOIN (SELECT l.recording_id,t.owner,b.object_id,t.id AS source_id,t.provider,t.asset_id,t.version,
 COALESCE(CAST(json_extract(o.document,'$.width') AS INTEGER),0) AS width,
 COALESCE(CAST(json_extract(o.document,'$.height') AS INTEGER),0) AS height
 FROM recording_source_links l CROSS JOIN tracks t ON t.owner=l.owner AND t.id=l.source_id`+currentTrackCoverJoins+`
 WHERE l.owner=? AND l.recording_id IN (SELECT value FROM json_each(?)) AND json_extract(t.document,'$.availability')='available') c
 ON c.owner=r.owner AND c.recording_id=r.id
 WHERE r.owner=? AND r.id IN (SELECT value FROM json_each(?))`, owner, string(raw), owner, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		var revision int64
		var manual *string
		var object, source, provider, asset, version sql.NullString
		var width, height sql.NullInt64
		if err = rows.Scan(&id, &revision, &manual, &object, &source, &provider, &asset, &version, &width, &height); err != nil {
			return nil, err
		}
		state := result[id]
		state.revision, state.manual = revision, manual
		if object.Valid {
			count++
			if len(state.candidates) == matching.MaxSources || count > catalog.MaxItems {
				return nil, domain.Fail("RATE_LIMITED", "Recording cover candidates exceed their resource budget")
			}
			w, h := int(width.Int64), int(height.Int64)
			if w < 1 || h < 1 || w > 8192 || h > 8192 || int64(w)*int64(h) > 16_000_000 {
				w, h = 0, 0
			}
			state.candidates = append(state.candidates, recordingCoverCandidate{RecordingCoverCandidate: library.RecordingCoverCandidate{ObjectID: object.String, SourceTrackID: source.String, ProviderInstanceID: provider.String, Width: w, Height: h}, asset: asset.String, version: version.String})
		}
		result[id] = state
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for id, state := range result {
		sort.Slice(state.candidates, func(i, j int) bool { return recordingCoverLess(state.candidates[i], state.candidates[j]) })
		result[id] = state
	}
	return result, nil
}

func (s *Store) RecordingCover(ctx context.Context, owner, id string) (library.RecordingCover, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return library.RecordingCover{}, err
	}
	defer tx.Rollback()
	states, err := readRecordingCoverStates(ctx, tx, owner, []string{id})
	if err != nil {
		return library.RecordingCover{}, err
	}
	state, ok := states[id]
	if !ok {
		return library.RecordingCover{}, domain.Fail("NOT_FOUND", "Recording not found")
	}
	return state.view(id), nil
}

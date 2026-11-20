package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

type albumMergeRecording struct {
	id, preferred string
	move          []string
	oldRecordings []string
}

func prepareAlbumMergeRecording(ctx context.Context, tx *sql.Tx, owner string, target music.Track, donors []music.Track) (albumMergeRecording, error) {
	group := albumMergeRecording{id: target.ID}
	var allSources []string
	var incoming []string
	preferred := target.SelectedSource
	for _, donor := range donors {
		if preferred == nil && donor.SelectedSource != nil {
			preferred = donor.SelectedSource
		}
		for _, s := range donor.Sources {
			incoming = append(incoming, s.SourceTrackID)
		}
		if len(donor.Sources) > 0 && donor.ID != donor.Sources[0].SourceTrackID {
			group.oldRecordings = append(group.oldRecordings, donor.ID)
		}
	}
	if len(incoming) == 0 {
		return group, nil
	}
	for _, s := range target.Sources {
		allSources = append(allSources, s.SourceTrackID)
	}
	allSources = append(allSources, incoming...)
	if len(allSources) > matching.MaxSources {
		return group, domain.Fail("RATE_LIMITED", "Recording member limit exceeded")
	}
	if err := visibleSources(ctx, tx, owner, allSources); err != nil {
		return group, err
	}
	if err := checkExclusions(ctx, tx, owner, allSources); err != nil {
		return group, err
	}
	group.move = incoming
	// 单来源曲目的 ID 必须留给解除关联后的源曲。新录音继承用户选中的主曲目资料。
	if len(target.Sources) > 0 && target.ID == target.Sources[0].SourceTrackID {
		var err error
		group.id, err = createRecording(ctx, tx, owner)
		if err != nil {
			return group, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO music_tracks(id,owner,title,origin,metadata_locked,created_at)
 SELECT ?,owner,title,'recording',1,created_at FROM music_tracks WHERE id=? AND owner=?`, group.id, target.ID, owner)
		if err != nil {
			return group, err
		}
		group.move = allSources
	}
	group.preferred = preferred.ID
	return group, nil
}

func (group albumMergeRecording) apply(ctx context.Context, tx *sql.Tx, owner string, expected int64) error {
	if len(group.move) == 0 {
		return nil
	}
	m := matchingMutation{tx: tx, owner: owner, result: matching.Mutation{Revision: expected + 1, OperationID: domain.NewID(), RecordingID: &group.id}}
	if err := m.moveSources(ctx, "link", group.move, group.id); err != nil {
		return err
	}
	// 主录音已选过封面时沿用；否则继承被合并录音的第一项人工封面。
	for _, id := range group.oldRecordings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recording_cover_preferences(owner,recording_id,revision,object_id)
 SELECT owner,?,1,object_id FROM recording_cover_preferences WHERE owner=? AND recording_id=? AND object_id IS NOT NULL
 ON CONFLICT(owner,recording_id) DO NOTHING`, group.id, owner, id); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO recording_default_sources(owner,recording_id,source_id) VALUES(?,?,?)
 ON CONFLICT(owner,recording_id) DO UPDATE SET source_id=excluded.source_id`, owner, group.id, group.preferred)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE recordings SET updated_at=? WHERE owner=? AND id=?", time.Now().UTC().Format(time.RFC3339Nano), owner, group.id); err != nil {
		return err
	}
	m.result.Changes = append(m.result.Changes, matching.LinkChange{Operation: "defaultSource", SourceTrackID: &group.preferred, FromRecordingID: &group.id, ToRecordingID: &group.id})
	return m.saveChanges(ctx)
}

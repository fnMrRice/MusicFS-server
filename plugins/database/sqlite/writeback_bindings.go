package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

// The bytes and original object provenance remain immutable. A fresh binding
// records that verified-identical audio now lives in a new metadata envelope.
func rebindWritebackContent(ctx context.Context, tx *sql.Tx, owner string, old mediastore.Selection, asset identification.Asset) error {
	revision, err := advanceContentRevision(ctx, tx, owner, asset.SourceTrackID)
	if err != nil {
		return err
	}
	for _, prior := range old.Bindings {
		if !prior.Current || prior.State != "available" {
			continue
		}
		binding := prior
		binding.ID = domain.NewID()
		binding.Target = mediastore.ContentTarget{SourceTrackID: asset.SourceTrackID, AssetID: asset.ID, AssetVersion: asset.Version}
		binding.CreatedAt = time.Now().UTC()
		raw, err := json.Marshal(binding)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE media_content_bindings SET current=0 WHERE owner=? AND id=?", owner, prior.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO media_content_bindings(id,owner,source_id,object_id,asset_id,field,language,current,document,created_at) VALUES(?,?,?,?,?,?,?,1,?,?)", binding.ID, owner, asset.SourceTrackID, binding.ObjectID, asset.ID, binding.Field, binding.Language, raw, binding.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE media_content_slots SET binding_id=? WHERE owner=? AND source_id=? AND field=? AND language=?", binding.ID, owner, asset.SourceTrackID, binding.Field, binding.Language)
		if err != nil {
			return err
		}
		if err = recordContentChange(ctx, tx, owner, asset.SourceTrackID, revision, "writeback", binding.Field, binding.Language, prior.ID, binding.ID, binding.ManualLocked); err != nil {
			return err
		}
	}
	return nil
}

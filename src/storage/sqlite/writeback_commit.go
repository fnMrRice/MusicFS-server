package sqlite

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

func validateWritebackResult(w writebackhost.Work, size int64, r writeback.Result, o library.Observation) error {
	if r.JobID != w.Job.ID || r.SourceTrackID != w.Target.SourceTrackID || r.Old.Version != w.Target.AssetVersion || r.Old.Identity != w.Target.FileIdentity || r.Old.Size != size || r.New.Size <= 0 || r.New.Identity == "" || r.New.Version == "" || !identification.ValidDigest(r.Old.SHA512) || !identification.ValidDigest(r.New.SHA512) || !identification.ValidDigest(r.AudioSHA512) {
		return domain.Fail("RESOURCE_CHANGED", "Writeback result does not match its execution binding")
	}
	if r.Changed && r.New.Version == r.Old.Version || !r.Changed && r.New != r.Old {
		return domain.Fail("CONFLICT", "Writeback change status is inconsistent")
	}
	if o.ExternalID != w.ExternalID || o.RootID != w.Target.RootID || o.RelativePath != w.Target.RelativePath || o.ObservedVersion != r.New.Version || o.SizeBytes != r.New.Size || o.MIME == "" {
		return domain.Fail("RESOURCE_CHANGED", "Writeback observation does not match its result")
	}
	return nil
}

func (s *Store) CommitWriteback(ctx context.Context, owner, id string, result writeback.Result, o library.Observation) (writebackhost.Outcome, error) {
	var empty writebackhost.Outcome
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	w, size, err := readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return empty, err
	}
	if w.Job.Status == "succeeded" && w.Job.Result != nil {
		var raw []byte
		var prior writeback.Result
		if err = tx.QueryRowContext(ctx, "SELECT commit_result FROM writeback_jobs WHERE owner=? AND id=?", owner, id).Scan(&raw); err != nil {
			return empty, err
		}
		if err = json.Unmarshal(raw, &prior); err != nil {
			return empty, err
		}
		if !reflect.DeepEqual(prior, result) {
			return empty, domain.Fail("CONFLICT", "Writeback commit replay changed its result")
		}
		return *w.Job.Result, nil
	}
	if w.Job.Status != "running" && w.Job.Status != "recovery" {
		return empty, domain.Fail("CONFLICT", "Writeback job is not running or recovering")
	}
	if err = validateWritebackWork(ctx, tx, w, size, w.Target, w.Job.Request.ExpectedRevision); err != nil {
		return empty, err
	}
	if err = validateWritebackResult(w, size, result, o); err != nil {
		return empty, err
	}
	track, err := writebackTrack(ctx, tx, owner, w.Target)
	if err != nil {
		return empty, err
	}
	if track.Asset.FileSHA512 != nil && *track.Asset.FileSHA512 != result.Old.SHA512 {
		return empty, domain.Fail("RESOURCE_CHANGED", "Writeback original digest changed")
	}
	selected, err := readContentSelection(ctx, tx, owner, track.ID)
	if err != nil {
		return empty, err
	}
	// Only the metadata envelope changed. The coordinator verified the entire
	// audio payload, so unchanged audio facts remain valid when no probe ran.
	if o.Codec == nil {
		o.Codec = track.Codec
	}
	if o.Container == nil {
		o.Container = track.Container
	}
	if o.DurationMs == nil {
		o.DurationMs = track.DurationMs
	}
	asset := *track.Asset
	if result.Changed {
		asset, err = scanAsset(ctx, tx, owner, track.ID, track.AssetID, o)
		if err != nil {
			return empty, err
		}
		if asset.ID == track.AssetID {
			return empty, domain.Fail("CONFLICT", "Changed writeback did not create a new asset")
		}
		track.AssetID = asset.ID
		track.Version = asset.Version
		track.Size = result.New.Size
		track.MIME = o.MIME
		if o.Title != nil {
			track.Title = o.Title
		}
		if o.Artists != nil {
			track.Artists = o.Artists
		}
		if o.DisplayName != "" {
			track.DisplayName = o.DisplayName
		}
		track.Codec, track.Container, track.DurationMs = o.Codec, o.Container, o.DurationMs
	}
	now := time.Now().UTC()
	spec := identification.Spec{Kind: "fileSha512", Origin: "probe", Algorithm: "sha512", AlgorithmVersion: "sha512-v1", ConfigRevision: "writeback-full-file-v1", Range: identification.Range{Unit: "bytes", End: &result.New.Size}}
	evidence := identification.Result{Status: "verified", SHA512: &result.New.SHA512, BitrateMode: "unknown", ComputedAt: &now}
	if err = evidence.Validate(spec); err != nil {
		return empty, err
	}
	if _, err = insertEvidence(ctx, tx, owner, asset, spec, evidence); err != nil {
		return empty, err
	}
	if err = saveAsset(ctx, tx, owner, asset); err != nil {
		return empty, err
	}
	asset, err = readAsset(ctx, tx, owner, asset.ID)
	if err != nil {
		return empty, err
	}
	track.Asset = &asset
	track.BitrateBps = asset.BitrateBps
	raw, err := json.Marshal(track)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE tracks SET version=?,asset_id=?,mime=?,size=?,document=? WHERE owner=? AND id=?", asset.Version, asset.ID, track.MIME, result.New.Size, raw, owner, track.ID); err != nil {
		return empty, err
	}
	if result.Changed {
		if err = rebindWritebackContent(ctx, tx, owner, selected, asset); err != nil {
			return empty, err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
		return empty, err
	}
	out := writebackhost.Outcome{Changed: result.Changed, PreviousAssetID: w.Target.AssetID, AssetID: asset.ID, AssetVersion: asset.Version, FileSHA512: result.New.SHA512}
	output, err := json.Marshal(out)
	if err != nil {
		return empty, err
	}
	committed, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE writeback_jobs SET state='succeeded',error=NULL,result=?,commit_result=?,updated_at=? WHERE owner=? AND id=?", output, committed, now.Format(time.RFC3339Nano), owner, id)
	if err != nil {
		return empty, err
	}
	return out, tx.Commit()
}

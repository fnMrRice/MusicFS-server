package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

func scanAsset(ctx context.Context, tx *sql.Tx, owner, track, previous string, o *library.Observation) (identification.Asset, error) {
	var a identification.Asset
	var err error
	if previous != "" {
		a, err = readAsset(ctx, tx, owner, previous)
		if err != nil {
			return a, err
		}
		if a.Current && (a.Version != o.ObservedVersion || a.FileSizeBytes == nil || *a.FileSizeBytes != o.SizeBytes) {
			if err := invalidateAsset(ctx, tx, owner, a.ID); err != nil {
				return a, err
			}
			a.Current = false
		}
	}
	if !a.Current {
		a = identification.Asset{ID: domain.NewID(), SourceTrackID: track, Version: o.ObservedVersion, EvidenceVersion: identification.EvidenceVersion, Current: true, FileSizeBytes: &o.SizeBytes, Evidence: []identification.Evidence{}, BitrateMode: "unknown", HashStatus: "unknown"}
		raw, err := json.Marshal(a)
		if err != nil {
			return a, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO source_assets(id,owner,track_id,version,current,document,created_at) VALUES(?,?,?,?,1,?,?)", a.ID, owner, track, a.Version, raw, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return a, err
		}
	}
	a.Codec, a.Container, a.DurationMs = o.Codec, o.Container, o.DurationMs
	inputs, err := library.EvidenceForObservation(*o)
	if err != nil {
		return a, err
	}
	if o.NetEaseID != "" || o.NetEaseIDConflict {
		// 同一版本的文件带有两个不同编号时保留冲突，后续的空探测不能将它降级成普通文字匹配。
		var stored sql.NullString
		err = tx.QueryRowContext(ctx, `INSERT INTO music_netease_asset_ids(asset_id,netease_id) VALUES(?,nullif(?,''))
			ON CONFLICT(asset_id) DO UPDATE SET netease_id=NULL WHERE music_netease_asset_ids.netease_id IS DISTINCT FROM excluded.netease_id
			RETURNING netease_id`, a.ID, o.NetEaseID).Scan(&stored)
		if err == nil && !stored.Valid {
			o.AlbumRelease = nil
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return a, err
		}
	}
	if err := reconcileScanEvidence(ctx, tx, owner, a.ID, inputs, o.ProbeStatus == "succeeded"); err != nil {
		return a, err
	}
	for _, input := range inputs {
		if err := input.Result.Validate(input.Spec); err != nil {
			return a, err
		}
		if err := input.Spec.Validate(a.FileSizeBytes); err != nil {
			return a, err
		}
		// Repeated identical observations do not create evidence revisions or
		// reset a complete-file verification on the same immutable asset.
		var previous []byte
		err := tx.QueryRowContext(ctx, "SELECT document FROM asset_evidence WHERE asset_id=? AND kind=? AND origin=? AND invalidated_at IS NULL ORDER BY generation DESC LIMIT 1", a.ID, input.Kind, input.Origin).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return a, err
		}
		if err == nil {
			var old identification.Evidence
			if err := json.Unmarshal(previous, &old); err != nil {
				return a, err
			}
			oldValue, newValue := old.Result, input.Result
			oldValue.ComputedAt, newValue.ComputedAt = nil, nil
			oldSpec, _ := json.Marshal(old.Spec)
			newSpec, _ := json.Marshal(input.Spec)
			oldResult, _ := json.Marshal(oldValue)
			newResult, _ := json.Marshal(newValue)
			if string(oldSpec) == string(newSpec) && string(oldResult) == string(newResult) {
				continue
			}
		}
		if _, err := insertEvidence(ctx, tx, owner, a, input.Spec, input.Result); err != nil {
			return a, err
		}
	}
	if err := saveAsset(ctx, tx, owner, a); err != nil {
		return a, err
	}
	return readAsset(ctx, tx, owner, a.ID)
}

// A full provider observation replaces its claims. Only a successful probe is
// authoritative for probe/estimate fields: failed or unattempted probes retain
// the last successful evidence for these unchanged bytes, never a new empty result.
func reconcileScanEvidence(ctx context.Context, tx *sql.Tx, owner, asset string, inputs []identification.Evidence, probed bool) error {
	owned := map[string][2]string{
		"bitrate/provider":        {"bitrate", "provider"},
		"providerSha512/provider": {"providerSha512", "provider"},
	}
	if probed {
		owned["bitrate/probe"] = [2]string{"bitrate", "probe"}
		owned["bitrate/estimate"] = [2]string{"bitrate", "estimate"}
	}
	seen := map[string]bool{}
	for _, e := range inputs {
		key := e.Kind + "/" + e.Origin
		if seen[key] {
			return identification.Invalid()
		}
		seen[key] = true
	}
	for key, field := range owned {
		if !seen[key] {
			if err := invalidateEvidence(ctx, tx, owner, asset, field[0], field[1]); err != nil {
				return err
			}
		}
	}
	return nil
}

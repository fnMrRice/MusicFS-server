package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

// PublishOnlineTrack 仅保存元数据；未观察音频时不填写大小、哈希或字节版本证据。
func (s *Store) PublishOnlineTrack(ctx context.Context, owner, provider string, metadata online.Track) (library.Track, error) {
	var empty library.Track
	if owner == "" || provider == "" || metadata.ExternalID == "" || metadata.Title == "" {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid online track publication")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var id, asset string
	err = tx.QueryRowContext(ctx, "SELECT id,asset_id FROM tracks WHERE owner=? AND provider=? AND external_id=?", owner, provider, metadata.ExternalID).Scan(&id, &asset)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	if id == "" {
		id = domain.NewID()
		asset = domain.NewID()
	}
	current := identification.Asset{ID: asset, SourceTrackID: id, Version: online.Unversioned, EvidenceVersion: identification.EvidenceVersion, Current: true, DurationMs: metadata.DurationMs, BitrateMode: "unknown", HashStatus: "unknown", Evidence: []identification.Evidence{}}
	raw, err := json.Marshal(current)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO source_assets(id,owner,track_id,version,current,document,created_at) VALUES(?,?,?,?,1,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document", asset, owner, id, online.Unversioned, raw, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return empty, err
	}
	track := library.Track{ID: id, ProviderID: provider, DisplayName: metadata.Title, Title: &metadata.Title, Artists: library.NormalizeArtists(metadata.Artists), DurationMs: metadata.DurationMs, Availability: "available", AssetID: asset, Asset: &current}
	raw, err = json.Marshal(track)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document", id, owner, provider, metadata.ExternalID, "online-audio", "", online.Unversioned, asset, "", 0, raw)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return track, nil
}

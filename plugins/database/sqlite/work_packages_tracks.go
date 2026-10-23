package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

// 只发布快照明确包含的音轨，不把包外或本次未观察到的来源标记为丢失。
func publishPackageTrack(ctx context.Context, tx *sql.Tx, owner string, p workpackages.Package, o library.Observation) (library.Track, error) {
	var id, asset string
	err := tx.QueryRowContext(ctx, "SELECT id,asset_id FROM tracks WHERE owner=? AND provider=? AND external_id=?", owner, p.ProviderID, o.ExternalID).Scan(&id, &asset)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return library.Track{}, err
	}
	if id == "" {
		id = domain.NewID()
	}
	current, err := scanAsset(ctx, tx, owner, id, asset, o)
	if err != nil {
		return library.Track{}, err
	}
	artists := library.NormalizeArtists(o.Artists)
	t := library.Track{ID: id, ProviderID: p.ProviderID, DisplayName: o.DisplayName, Title: o.Title, Artists: artists, DurationMs: o.DurationMs,
		Codec: o.Codec, Container: o.Container, BitrateBps: current.BitrateBps, Availability: "available", AssetID: current.ID, Asset: &current,
		Album: o.Album, Genre: o.Genre, Year: o.Year, SampleRateHz: o.SampleRateHz}
	raw, err := json.Marshal(t)
	if err != nil {
		return library.Track{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
		VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET root_id=excluded.root_id,path=excluded.path,version=excluded.version,
		asset_id=excluded.asset_id,mime=excluded.mime,size=excluded.size,document=excluded.document`, id, owner, p.ProviderID,
		o.ExternalID, o.RootID, o.RelativePath, o.ObservedVersion, current.ID, o.MIME, o.SizeBytes, raw)
	return t, err
}

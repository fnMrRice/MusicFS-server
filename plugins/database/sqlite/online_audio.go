package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

// 只有库内文档保存平台原图地址。旧文档没有该字段时由首次展示补取元数据。
type storedTrack struct {
	library.Track
	CoverURL *string `json:"_providerCoverUrl,omitempty"`
}

const upsertOnlineTrack = `INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
 VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document`

// PublishOnlineTrack 仅保存元数据；未观察音频时不填写大小、哈希或字节版本证据。
func (s *Store) PublishOnlineTrack(ctx context.Context, owner, provider string, metadata online.Track) (library.Track, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return library.Track{}, err
	}
	defer tx.Rollback()
	writeTrack, err := tx.PrepareContext(ctx, upsertOnlineTrack)
	if err != nil {
		return library.Track{}, err
	}
	defer writeTrack.Close()
	var additions []catalogAddition
	track, err := publishOnlineTrack(ctx, tx, writeTrack, owner, provider, metadata, "available", &additions)
	if err != nil {
		return library.Track{}, err
	}
	if err = attachNewCatalogSources(ctx, tx, owner, provider, additions); err != nil {
		return library.Track{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
		return library.Track{}, err
	}
	if err = populateTrackCovers(ctx, tx, owner, &track); err != nil {
		return library.Track{}, err
	}
	return track, tx.Commit()
}

func publishOnlineTrack(ctx context.Context, tx *sql.Tx, writeTrack *sql.Stmt, owner, provider string, metadata online.Track, availability string, additions *[]catalogAddition) (library.Track, error) {
	var empty library.Track
	if owner == "" || provider == "" || metadata.ExternalID == "" || metadata.Title == "" {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid online track publication")
	}
	var id, asset string
	var previousRaw []byte
	err := tx.QueryRowContext(ctx, "SELECT id,asset_id,document FROM tracks WHERE owner=? AND provider=? AND external_id=?", owner, provider, metadata.ExternalID).Scan(&id, &asset, &previousRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	created := id == ""
	if created {
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
	track := library.Track{ID: id, ProviderID: provider, DisplayName: metadata.Title, Title: &metadata.Title, Artists: library.NormalizeArtists(metadata.Artists), DurationMs: metadata.DurationMs, Availability: availability, AssetID: asset, Asset: &current}
	if album := strings.TrimSpace(metadata.Album); album != "" {
		track.Album = &album
	}
	track.AlbumRelease, err = storedNativeAlbumRelease(ctx, tx, provider, metadata, previousRaw)
	if err != nil {
		return empty, err
	}
	var cover string
	if metadata.CoverURL != nil {
		cover = library.NetEaseArtworkURL(*metadata.CoverURL)
	}
	raw, err = json.Marshal(storedTrack{Track: track, CoverURL: &cover})
	if err != nil {
		return empty, err
	}
	_, err = writeTrack.ExecContext(ctx, id, owner, provider, metadata.ExternalID, "online-audio", "", online.Unversioned, asset, "", 0, raw)
	if err != nil {
		return empty, err
	}
	if created && availability == "available" {
		*additions = append(*additions, catalogAddition{id, catalogIdentity(metadata)})
	}
	return track, nil
}

// storedNativeAlbumRelease 对照已注册平台、歌曲身份与库内该音源的原始曲目标签。
func storedNativeAlbumRelease(ctx context.Context, tx *sql.Tx, provider string, metadata online.Track, previousRaw []byte) (*audiotypes.AlbumRelease, error) {
	r := metadata.AlbumRelease
	if r == nil {
		return nil, nil
	}
	if metadata.NativeAlbumRelease(r.Namespace, r.ID) == nil {
		return nil, domain.Fail("PROTOCOL_MISMATCH", "Invalid native release metadata")
	}
	var registered bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM music_source_providers WHERE provider_instance_id=? AND plugin_id=?)", provider, pluginproto.OnlinePluginID(r.Namespace)).Scan(&registered)
	if err != nil || !registered {
		return nil, err
	}
	if previousRaw == nil {
		return r, nil
	}
	var previous library.Track
	if err := json.Unmarshal(previousRaw, &previous); err != nil {
		return nil, err
	}
	if previous.Title == nil {
		return nil, nil
	}
	if previous.Availability == "missing" && len(previous.Artists) == 0 && *previous.Title == metadata.ExternalID {
		return r, nil
	}
	if audiotypes.SameTrackCredit(*previous.Title, previous.Artists, metadata.Title, metadata.Artists) {
		return r, nil
	}
	// 旧音源文档按斜杠展开了 AC/DC 等完整署名，只有完整展开名单一致时才接受。
	if audiotypes.NormalizeMatchName(*previous.Title) == audiotypes.NormalizeMatchName(metadata.Title) &&
		slices.Equal(previous.Artists, library.NormalizeArtists(metadata.Artists)) {
		return r, nil
	}
	return nil, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

type catalogAddition struct {
	source   string
	identity matching.CatalogIdentity
}

func catalogIdentity(t online.Track) matching.CatalogIdentity {
	id := matching.CatalogIdentity{Title: t.Title, Artists: t.Artists, Album: t.Album, NetEaseID: t.ExternalID}
	if t.DurationMs != nil {
		id.DurationMs = *t.DurationMs
	}
	return id
}

type localCatalogMember struct {
	target   string
	identity matching.CatalogIdentity
}

// 只索引当前用户的可见文件。每个文件使用自己的标签，已有录音的不同来源共享规范曲目身份。
func localCatalogIndex(ctx context.Context, tx *sql.Tx, owner string) (map[string][]localCatalogMember, error) {
	index := map[string][]localCatalogMember{}
	err := musicRows(ctx, tx, ordinaryMusicSources+`SELECT s.track_id,coalesce(n.netease_id,''),
		coalesce(json_extract(t.document,'$.title'),''),coalesce(json_extract(t.document,'$.artists'),'[]'),
		coalesce(json_extract(t.document,'$.album'),''),coalesce(json_extract(t.document,'$.durationMs'),0)
		FROM tracks t JOIN music_sources s ON s.id=t.id AND s.owner=t.owner
		JOIN source_assets a ON a.id=t.asset_id AND a.owner=t.owner AND a.track_id=t.id AND a.current=1 AND a.version=t.version
		LEFT JOIN music_netease_asset_ids n ON n.asset_id=a.id
		WHERE t.owner=? AND t.path<>'' AND json_valid(t.document) AND json_extract(t.document,'$.availability')='available'
		AND (n.asset_id IS NULL OR n.netease_id IS NOT NULL)
		AND `+ordinaryMusicTrack, func(rows *sql.Rows) error {
		var m localCatalogMember
		var artists string
		if err := rows.Scan(&m.target, &m.identity.NetEaseID, &m.identity.Title, &artists, &m.identity.Album, &m.identity.DurationMs); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(artists), &m.identity.Artists); err != nil {
			return err
		}
		for _, key := range m.identity.Keys() {
			index[key] = append(index[key], m)
		}
		return nil
	}, owner, owner)
	return index, err
}

func uniqueCatalogTarget(index map[string][]localCatalogMember, incoming matching.CatalogIdentity) string {
	explicit, textual := map[string]bool{}, map[string]bool{}
	for _, key := range incoming.Keys() {
		for _, m := range index[key] {
			if !incoming.Match(m.identity) {
				continue
			}
			if incoming.NetEaseID != "" && incoming.NetEaseID == m.identity.NetEaseID {
				explicit[m.target] = true
			} else {
				textual[m.target] = true
			}
		}
	}
	if len(explicit) > 0 {
		textual = explicit
	}
	if len(textual) == 1 {
		for id := range textual {
			return id
		}
	}
	return ""
}

// 调用方只传本事务新建的可用来源。重放或更新既有来源不会启动归并。
func attachNewCatalogSources(ctx context.Context, tx *sql.Tx, owner, provider string, additions []catalogAddition) error {
	if len(additions) == 0 {
		return nil
	}
	var netease bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM music_source_providers WHERE provider_instance_id=? AND plugin_id=?)",
		provider, pluginproto.OnlinePluginID(online.Netease)).Scan(&netease); err != nil || !netease {
		return err
	}
	index, err := localCatalogIndex(ctx, tx, owner)
	if err != nil {
		return err
	}
	plans := map[string][]catalogAddition{}
	for _, a := range additions {
		if id := uniqueCatalogTarget(index, a.identity); id != "" {
			plans[id] = append(plans[id], a)
		}
	}
	if len(plans) == 0 {
		return nil
	}
	revision, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		return err
	}
	m := matchingMutation{owner: owner, tx: tx, result: matching.Mutation{Revision: revision + 1, OperationID: domain.NewID()}}
	targets := make([]string, 0, len(plans))
	for id := range plans {
		targets = append(targets, id)
	}
	sort.Strings(targets)
	for _, target := range targets {
		if err := attachCatalogGroup(ctx, &m, target, plans[target]); err != nil {
			return err
		}
	}
	if len(m.result.Changes) == 0 {
		return nil
	}
	return m.saveChanges(ctx)
}

func attachCatalogGroup(ctx context.Context, m *matchingMutation, target string, additions []catalogAddition) error {
	var members []string
	var knownID string
	standalone, blocked := false, false
	err := musicRows(ctx, m.tx, `SELECT s.id,l.decision,m.metadata_locked,n.asset_id IS NOT NULL AND n.netease_id IS NULL,
		CASE WHEN t.path='' AND t.root_id='online-audio' AND p.plugin_id=? THEN t.external_id ELSE coalesce(n.netease_id,'') END
		FROM music_sources s JOIN tracks t ON t.owner=s.owner AND t.id=s.id JOIN music_tracks m ON m.id=s.track_id
		LEFT JOIN recording_source_links l ON l.source_id=s.id AND l.owner=s.owner
		LEFT JOIN music_source_providers p ON p.provider_instance_id=t.provider
		LEFT JOIN music_netease_asset_ids n ON n.asset_id=t.asset_id WHERE s.owner=? AND s.track_id=? ORDER BY s.id`, func(rows *sql.Rows) error {
		var id, evidence string
		var decision sql.NullString
		var locked, conflictingTags bool
		if err := rows.Scan(&id, &decision, &locked, &conflictingTags, &evidence); err != nil {
			return err
		}
		members = append(members, id)
		standalone = standalone || id == target
		// 已有录音只追加来源；不迁移显式解除的成员、独立锁定的资料或包含另一平台编号的组。
		blocked = blocked || conflictingTags || decision.String == "unlinked" || id == target && locked
		if evidence != "" {
			blocked = blocked || knownID != "" && knownID != evidence
			knownID = evidence
		}
		return nil
	}, pluginproto.OnlinePluginID(online.Netease), m.owner, target)
	if err != nil || blocked || len(members) == 0 {
		return err
	}
	var sources []string
	for _, a := range additions {
		if knownID != "" && knownID != a.identity.NetEaseID {
			continue
		}
		// 没有已证实的编号时，同批中互不相同的网易云曲目不能借同一份文件标签合并。
		if knownID == "" && a.identity.NetEaseID != additions[0].identity.NetEaseID {
			return nil
		}
		sources = append(sources, a.source)
	}
	if len(sources) == 0 || len(members)+len(sources) > matching.MaxSources {
		return nil
	}
	if knownID == "" {
		knownID = additions[0].identity.NetEaseID
	}
	// 同一平台曲目可能从另一个网易云实例再次导入。此前已拒绝该曲目和目标组成员的关系时沿用排除。
	var excluded bool
	err = m.tx.QueryRowContext(ctx, `WITH equivalent AS (
		SELECT t.id FROM tracks t JOIN music_source_providers p ON p.provider_instance_id=t.provider
		WHERE t.owner=? AND t.external_id=? AND t.root_id='online-audio' AND t.path='' AND p.plugin_id=?),
		target_members AS (SELECT id FROM music_sources WHERE owner=? AND track_id=?)
		SELECT EXISTS(SELECT 1 FROM match_exclusions WHERE owner=? AND excluded=1 AND
		((left_source IN target_members AND right_source IN equivalent) OR (right_source IN target_members AND left_source IN equivalent)))`,
		m.owner, knownID, pluginproto.OnlinePluginID(online.Netease), m.owner, target, m.owner).Scan(&excluded)
	if err != nil || excluded {
		return err
	}
	if err := checkExclusions(ctx, m.tx, m.owner, append(members, sources...)); err != nil {
		var conflict *domain.Error
		if errors.As(err, &conflict) && conflict.Code == "CONFLICT" {
			return nil
		}
		return err
	}
	if standalone {
		recording, err := createRecording(ctx, m.tx, m.owner)
		if err != nil {
			return err
		}
		if err := m.moveSources(ctx, "link", members, recording); err != nil {
			return err
		}
		if err := moveTrackRelations(ctx, m.tx, m.owner, recording, target); err != nil {
			return err
		}
		// 原文件身份留给日后解除关联；专辑曲位已转交新录音，不能留下第二份手工位置。
		if _, err := m.tx.ExecContext(ctx, "DELETE FROM music_manual_album_tracks WHERE track_id=?", target); err != nil {
			return err
		}
		if _, err := m.tx.ExecContext(ctx, "UPDATE music_albums SET revision=revision+1 WHERE owner=? AND id IN (SELECT album_id FROM music_album_tracks WHERE track_id=?)", m.owner, recording); err != nil {
			return err
		}
		target = recording
	}
	return m.moveSources(ctx, "link", sources, target)
}

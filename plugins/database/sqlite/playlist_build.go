package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type playlistGroup struct {
	recording *string
	members   []playlists.SourceRef
}
type playlistBuilder struct {
	ctx      context.Context
	tx       *sql.Tx
	owner    string
	matching int64
	active   map[string]bool
	cache    map[string]playlists.AggregateSnapshot
	groups   map[string]playlistGroup
	nodes    int
}

func newPlaylistBuilder(ctx context.Context, tx *sql.Tx, owner string) (*playlistBuilder, error) {
	revision, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	return &playlistBuilder{ctx: ctx, tx: tx, owner: owner, matching: revision, active: map[string]bool{}, cache: map[string]playlists.AggregateSnapshot{}, groups: map[string]playlistGroup{}}, nil
}
func (b *playlistBuilder) group(source playlists.SourceRef) (playlistGroup, error) {
	if g, ok := b.groups[source.Key()]; ok {
		return g, nil
	}
	recording, members, err := playlistSourceGroup(b.ctx, b.tx, b.owner, source)
	if err != nil {
		return playlistGroup{}, err
	}
	g := playlistGroup{recording, members}
	for _, member := range members {
		b.groups[member.Key()] = g
	}
	return g, nil
}
func (b *playlistBuilder) build(id string, depth int) (playlists.AggregateSnapshot, error) {
	var empty playlists.AggregateSnapshot
	if b.active[id] {
		return empty, domain.Fail("CONFLICT", "Playlist bindings contain a cycle")
	}
	if snapshot, ok := b.cache[id]; ok {
		return snapshot, nil
	}
	b.nodes++
	if b.nodes > playlists.MaxGraphPlaylists || depth > playlists.MaxGraphDepth {
		return empty, domain.Fail("RATE_LIMITED", "Playlist graph exceeds its limit")
	}
	b.active[id] = true
	defer delete(b.active, id)
	p, err := readPersonalPlaylist(b.ctx, b.tx, b.owner, id)
	if err != nil {
		return empty, err
	}
	contributions, deps, childConfirm, err := b.contributions(p, depth)
	if err != nil {
		return empty, err
	}
	confirmation, err := b.applyHidden(p.ID, contributions)
	if err != nil {
		return empty, err
	}
	confirmation = append(confirmation, childConfirm...)
	rows := []playlists.AggregateEntry{}
	indices := map[string]int{}
	for _, provenance := range contributions {
		group, e := b.group(provenance.Source)
		if e != nil {
			return empty, e
		}
		key := provenance.Source.Key()
		if group.recording != nil {
			key = "recording:" + *group.recording
		}
		index, exists := indices[key]
		if !exists {
			index = len(rows)
			indices[key] = index
			sum := sha256.Sum256([]byte(id + "\x00" + key))
			rows = append(rows, playlists.AggregateEntry{ID: hex.EncodeToString(sum[:24]), Position: index, RecordingID: group.recording, Source: provenance.Source, Hidden: true, Provenance: []playlists.Provenance{}})
		}
		row := &rows[index]
		if row.Hidden && !provenance.Hidden {
			row.Source = provenance.Source
		}
		row.Hidden = row.Hidden && provenance.Hidden
		row.Provenance = append(row.Provenance, provenance)
	}
	for i := range rows {
		if err = selectPlaylistVersion(b.ctx, b.tx, b.owner, &rows[i]); err != nil {
			return empty, err
		}
	}
	// 扫描更新码率或大小不推进人工修订；先重算来源，再决定是否复用旧快照。
	if p.SnapshotRevision > 0 {
		prior, e := readPersonalSnapshot(b.ctx, b.tx, b.owner, id, p.SnapshotRevision)
		if e != nil {
			return empty, e
		}
		if prior.RuleRevision == p.RuleRevision && prior.HideRevision == p.HideRevision && prior.MatchingRevision == b.matching &&
			reflect.DeepEqual(prior.Dependencies, deps) && reflect.DeepEqual(prior.Entries, rows) {
			b.cache[id] = prior
			return prior, nil
		}
	}
	snapshot := playlists.AggregateSnapshot{PlaylistID: id, Revision: p.SnapshotRevision + 1, RuleRevision: p.RuleRevision, HideRevision: p.HideRevision, MatchingRevision: b.matching, Dependencies: deps, Entries: rows, NeedsConfirmation: uniquePlaylistStrings(confirmation), CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return empty, err
	}
	if _, err = b.tx.ExecContext(b.ctx, "INSERT INTO personal_playlist_snapshots VALUES(?,?,?,?)", id, snapshot.Revision, b.owner, raw); err != nil {
		return empty, err
	}
	p.SnapshotRevision = snapshot.Revision
	p.Stale = false
	if err = savePersonalPlaylist(b.ctx, b.tx, b.owner, &p, false); err != nil {
		return empty, err
	}
	b.cache[id] = snapshot
	return snapshot, nil
}
func uniquePlaylistStrings(input []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, id := range input {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
func (b *playlistBuilder) contributions(p playlists.Playlist, depth int) ([]playlists.Provenance, []playlists.Dependency, []string, error) {
	items := []playlists.Provenance{}
	deps := []playlists.Dependency{}
	confirm := []string{}
	appendItem := func(provenance playlists.Provenance) error {
		if len(items) >= playlists.MaxContributions || len(provenance.BindingPath) > playlists.MaxGraphDepth {
			return domain.Fail("RATE_LIMITED", "Playlist contribution count exceeds its limit")
		}
		if _, err := b.group(provenance.Source); err != nil {
			return err
		}
		items = append(items, provenance)
		return nil
	}
	for _, binding := range p.Bindings {
		switch binding.Kind {
		case "local":
			for _, entry := range binding.Entries {
				if err := appendItem(playlists.Provenance{BindingPath: []string{binding.ID}, OriginPlaylistID: p.ID, EntryID: entry.ID, Source: entry.Source, State: "present"}); err != nil {
					return nil, nil, nil, err
				}
			}
		case "remote":
			remote, err := readRemotePlaylist(b.tx.QueryRowContext(b.ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", b.owner, *binding.TargetID))
			if err != nil {
				return nil, nil, nil, err
			}
			if remote.Revision == 0 {
				return nil, nil, nil, domain.Fail("TEMPORARY_FAILURE", "Remote playlist has no completed snapshot")
			}
			snapshot, err := readRemoteSnapshot(b.ctx, b.tx, b.owner, remote.ID, remote.Revision)
			if err != nil {
				return nil, nil, nil, err
			}
			deps = append(deps, playlists.Dependency{Kind: "remote", ID: remote.ID, Revision: remote.Revision})
			for _, entry := range snapshot.Entries {
				source := entry.Source.ID
				if err = appendItem(playlists.Provenance{BindingPath: []string{binding.ID}, OriginPlaylistID: remote.ID, EntryID: entry.ID, Source: playlists.SourceRef{RemoteSourceID: &source}, State: entry.State}); err != nil {
					return nil, nil, nil, err
				}
			}
		case "playlist":
			child, err := b.build(*binding.TargetID, depth+1)
			if err != nil {
				return nil, nil, nil, err
			}
			deps = append(deps, playlists.Dependency{Kind: "playlist", ID: child.PlaylistID, Revision: child.Revision})
			confirm = append(confirm, child.NeedsConfirmation...)
			for _, row := range child.Entries {
				for _, provenance := range row.Provenance {
					provenance.BindingPath = append([]string{binding.ID}, provenance.BindingPath...)
					if err = appendItem(provenance); err != nil {
						return nil, nil, nil, err
					}
				}
			}
		default:
			return nil, nil, nil, playlists.Invalid()
		}
	}
	return items, deps, confirm, nil
}
func (s *Store) RebuildPlaylist(ctx context.Context, owner, id string, r playlists.RebuildRequest, key string) (playlists.AggregateSnapshot, error) {
	var result playlists.AggregateSnapshot
	if r.ExpectedRuleRevision < 1 || !playlists.ValidID(id) {
		return result, playlists.Invalid()
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "rebuild/"+id, key, r)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		if err != nil {
			return result, err
		}
		return result, authorizePlaylistSnapshot(ctx, m.tx, owner, result)
	}
	p, err := readPersonalPlaylist(ctx, m.tx, owner, id)
	if err != nil {
		return result, err
	}
	if p.RuleRevision != r.ExpectedRuleRevision {
		return result, domain.Fail("CONFLICT", "Playlist rules changed")
	}
	builder, err := newPlaylistBuilder(ctx, m.tx, owner)
	if err != nil {
		return result, err
	}
	result, err = builder.build(id, 1)
	if err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}

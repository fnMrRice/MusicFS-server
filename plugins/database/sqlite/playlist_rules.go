package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func canonicalPlaylistBindings(in []playlists.PlaylistBinding) ([]playlists.PlaylistBinding, error) {
	if err := playlists.ValidateBindings(in); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	out := []playlists.PlaylistBinding{}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []playlists.PlaylistBinding{}
	}
	for i := range out {
		if out[i].Entries == nil {
			out[i].Entries = []playlists.LocalEntry{}
		}
	}
	return out, nil
}
func assignPlaylistBindings(old, bindings []playlists.PlaylistBinding) error {
	prior := map[string]playlists.PlaylistBinding{}
	for _, b := range old {
		prior[b.ID] = b
	}
	for i := range bindings {
		b := &bindings[i]
		var before playlists.PlaylistBinding
		if b.ID == "" {
			b.ID = domain.NewID()
		} else {
			var ok bool
			before, ok = prior[b.ID]
			if !ok || b.Kind != before.Kind || !reflect.DeepEqual(b.TargetID, before.TargetID) {
				return domain.Fail("CONFLICT", "A playlist binding cannot change its original source")
			}
		}
		entries := map[string]playlists.SourceRef{}
		for _, e := range before.Entries {
			entries[e.ID] = e.Source
		}
		for j := range b.Entries {
			e := &b.Entries[j]
			if e.ID == "" {
				e.ID = domain.NewID()
			} else if previous, ok := entries[e.ID]; !ok || previous.Key() != e.Source.Key() &&
				(previous.TrackID == nil || e.Source.TrackID == nil || *previous.TrackID != *e.Source.TrackID) {
				return domain.Fail("CONFLICT", "A playlist occurrence cannot change its original source")
			}
		}
	}
	return nil
}
func validatePlaylistGraph(ctx context.Context, tx *sql.Tx, owner, id string) error {
	heights, active := map[string]int{}, map[string]bool{}
	var visit func(string) (int, error)
	visit = func(id string) (int, error) {
		if active[id] {
			return 0, domain.Fail("CONFLICT", "Playlist bindings contain a cycle")
		}
		if height, ok := heights[id]; ok {
			return height, nil
		}
		if len(heights)+len(active) >= playlists.MaxGraphPlaylists || len(active) >= playlists.MaxGraphDepth {
			return 0, domain.Fail("RATE_LIMITED", "Playlist binding graph exceeds its limit")
		}
		p, err := readPersonalPlaylist(ctx, tx, owner, id)
		if err != nil {
			return 0, err
		}
		active[id] = true
		defer delete(active, id)
		height := 1
		for _, b := range p.Bindings {
			if b.Kind == "playlist" {
				child, e := visit(*b.TargetID)
				if e != nil {
					return 0, e
				}
				height = max(height, child+1)
			}
		}
		if height > playlists.MaxGraphDepth {
			return 0, domain.Fail("RATE_LIMITED", "Playlist binding graph exceeds its limit")
		}
		heights[id] = height
		return height, nil
	}
	_, err := visit(id)
	return err
}
func validatePlaylistBindings(ctx context.Context, tx *sql.Tx, owner string, bindings []playlists.PlaylistBinding) error {
	sources := playlistSourceIDs{}
	for _, b := range bindings {
		switch b.Kind {
		case "local":
			for _, e := range b.Entries {
				if err := sources.add(e.Source); err != nil {
					return err
				}
			}
		case "remote":
			if _, err := readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, *b.TargetID)); err != nil {
				return err
			}
		case "playlist":
			if _, err := readPersonalPlaylist(ctx, tx, owner, *b.TargetID); err != nil {
				return err
			}
		}
	}
	return sources.checkVisible(ctx, tx, owner)
}
func (s *Store) CreatePlaylist(ctx context.Context, owner string, r playlists.CreatePlaylistRequest, key string) (playlists.Playlist, error) {
	var result playlists.Playlist
	var err error
	if r.Name, err = playlists.PlaylistName(r.Name); err != nil {
		return result, err
	}
	if r.Bindings, err = canonicalPlaylistBindings(r.Bindings); err != nil {
		return result, err
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "create", key, r)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		return result, err
	}
	if err = assignPlaylistBindings(nil, r.Bindings); err != nil {
		return result, err
	}
	if err = validatePlaylistBindings(ctx, m.tx, owner, r.Bindings); err != nil {
		return result, err
	}
	result = playlists.Playlist{Kind: "user", ID: domain.NewID(), Name: r.Name, RuleRevision: 1, Stale: true, Bindings: r.Bindings, CreatedAt: time.Now().UTC()}
	if err = savePersonalPlaylist(ctx, m.tx, owner, &result, true); err != nil {
		return result, err
	}
	if err = saveMusicPlaylistEntries(ctx, m.tx, owner, result); err != nil {
		return result, err
	}
	if err = validatePlaylistGraph(ctx, m.tx, owner, result.ID); err != nil {
		return result, err
	}
	if err = playlistAudit(ctx, m.tx, owner, result, "create", nil); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}
func (s *Store) EditPlaylist(ctx context.Context, owner, id string, r playlists.EditPlaylistRequest, key string) (playlists.Playlist, error) {
	var result playlists.Playlist
	var err error
	if r.ExpectedRevision < 1 || !playlists.ValidID(id) {
		return result, playlists.Invalid()
	}
	if r.Name, err = playlists.PlaylistName(r.Name); err != nil {
		return result, err
	}
	if r.Bindings, err = canonicalPlaylistBindings(r.Bindings); err != nil {
		return result, err
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "edit/"+id, key, r)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		return result, err
	}
	result, err = readPersonalPlaylist(ctx, m.tx, owner, id)
	if err != nil {
		return result, err
	}
	if result.RuleRevision != r.ExpectedRevision {
		return result, domain.Fail("CONFLICT", "Playlist rules changed")
	}
	if err = assignPlaylistBindings(result.Bindings, r.Bindings); err != nil {
		return result, err
	}
	if err = validatePlaylistBindings(ctx, m.tx, owner, r.Bindings); err != nil {
		return result, err
	}
	if err = queuePlaylistEntries(ctx, m.tx, owner, id, result.Bindings, r.Bindings); err != nil {
		return result, err
	}
	result.Name, result.Bindings, result.Stale = r.Name, r.Bindings, true
	result.RuleRevision++
	if err = savePersonalPlaylist(ctx, m.tx, owner, &result, false); err != nil {
		return result, err
	}
	if err = saveMusicPlaylistEntries(ctx, m.tx, owner, result); err != nil {
		return result, err
	}
	if err = validatePlaylistGraph(ctx, m.tx, owner, id); err != nil {
		return result, err
	}
	if err = playlistAudit(ctx, m.tx, owner, result, "edit", nil); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func playlistSelectionBinding(ctx context.Context, tx *sql.Tx, owner string, selection library.TrackSelection) (playlists.PlaylistBinding, error) {
	binding := playlists.PlaylistBinding{Kind: "local", Entries: []playlists.LocalEntry{}}
	tracks, err := resolveTrackSelection(ctx, tx, owner, selection)
	if err != nil {
		return binding, err
	}
	if len(tracks) == 0 {
		return binding, domain.Fail("INVALID_ARGUMENT", "Track selection is empty")
	}
	if len(tracks) > playlists.MaxEntries {
		return binding, domain.Fail("RATE_LIMITED", "Playlist entry limit exceeded")
	}
	for _, track := range tracks {
		binding.Entries = append(binding.Entries, playlists.LocalEntry{Source: playlists.SourceRef{SourceTrackID: strptr(track.ID)}})
	}
	return binding, nil
}

func (s *Store) CreatePlaylistSelection(ctx context.Context, owner string, request playlists.CreateSelectionRequest, key string) (playlists.SelectionResult, error) {
	var result playlists.SelectionResult
	var err error
	if request.Name, err = playlists.PlaylistName(request.Name); err != nil {
		return result, err
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "selection/create", key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		return result, err
	}
	binding, err := playlistSelectionBinding(ctx, m.tx, owner, request.Selection)
	if err != nil {
		return result, err
	}
	p := playlists.Playlist{ID: domain.NewID(), Name: request.Name, RuleRevision: 1, Stale: true, Bindings: []playlists.PlaylistBinding{binding}, CreatedAt: time.Now().UTC()}
	if err = assignPlaylistBindings(nil, p.Bindings); err != nil {
		return result, err
	}
	if err = savePersonalPlaylist(ctx, m.tx, owner, &p, true); err != nil {
		return result, err
	}
	if err = playlistAudit(ctx, m.tx, owner, p, "create", nil); err != nil {
		return result, err
	}
	result = playlists.SelectionResult{PlaylistID: p.ID, RuleRevision: p.RuleRevision, Count: len(binding.Entries)}
	return result, m.finish(ctx, result)
}

func (s *Store) AppendPlaylistSelection(ctx context.Context, owner, id string, request playlists.AppendSelectionRequest, key string) (playlists.SelectionResult, error) {
	var result playlists.SelectionResult
	if request.ExpectedRevision < 1 || !playlists.ValidID(id) {
		return result, playlists.Invalid()
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "selection/append/"+id, key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		return result, err
	}
	p, err := readPersonalPlaylist(ctx, m.tx, owner, id)
	if err != nil {
		return result, err
	}
	if p.RuleRevision != request.ExpectedRevision {
		return result, domain.Fail("CONFLICT", "Playlist rules changed")
	}
	binding, err := playlistSelectionBinding(ctx, m.tx, owner, request.Selection)
	if err != nil {
		return result, err
	}
	bindings := append(append([]playlists.PlaylistBinding{}, p.Bindings...), binding)
	if err = playlists.ValidateBindings(bindings); err != nil {
		return result, err
	}
	if err = assignPlaylistBindings(p.Bindings, bindings); err != nil {
		return result, err
	}
	if err = queuePlaylistEntries(ctx, m.tx, owner, id, nil, bindings[len(bindings)-1:]); err != nil {
		return result, err
	}
	p.Bindings, p.Stale = bindings, true
	p.RuleRevision++
	if err = savePersonalPlaylist(ctx, m.tx, owner, &p, false); err != nil {
		return result, err
	}
	if err = validatePlaylistGraph(ctx, m.tx, owner, id); err != nil {
		return result, err
	}
	if err = playlistAudit(ctx, m.tx, owner, p, "edit", nil); err != nil {
		return result, err
	}
	result = playlists.SelectionResult{PlaylistID: p.ID, RuleRevision: p.RuleRevision, Count: len(binding.Entries)}
	return result, m.finish(ctx, result)
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func authorizePlaylistSnapshot(ctx context.Context, tx *sql.Tx, owner string, snapshot playlists.AggregateSnapshot) error {
	if _, err := readPersonalPlaylist(ctx, tx, owner, snapshot.PlaylistID); err != nil {
		return err
	}
	seen := map[string]bool{}
	count := 0
	for _, entry := range snapshot.Entries {
		for _, p := range entry.Provenance {
			count++
			if count > playlists.MaxContributions {
				return domain.Fail("RATE_LIMITED", "Playlist snapshot exceeds its limit")
			}
			if seen[p.Source.Key()] {
				continue
			}
			seen[p.Source.Key()] = true
			if err := playlistSourceVisible(ctx, tx, owner, p.Source); err != nil {
				return err
			}
		}
	}
	return nil
}

type playlistStaleState struct {
	active map[string]bool
	done   map[string]bool
	nodes  int
}

func newPlaylistStaleState() *playlistStaleState {
	return &playlistStaleState{active: map[string]bool{}, done: map[string]bool{}}
}
func personalPlaylistStale(ctx context.Context, tx *sql.Tx, owner string, p playlists.Playlist, matching int64, state *playlistStaleState, depth int) (stale bool, err error) {
	if depth > playlists.MaxGraphDepth {
		return true, domain.Fail("RATE_LIMITED", "Playlist graph exceeds its limit")
	}
	if state.active[p.ID] {
		return true, domain.Fail("CONFLICT", "Playlist bindings contain a cycle")
	}
	if result, ok := state.done[p.ID]; ok {
		return result, nil
	}
	state.nodes++
	if state.nodes > playlists.MaxGraphPlaylists {
		return true, domain.Fail("RATE_LIMITED", "Playlist graph exceeds its limit")
	}
	state.active[p.ID] = true
	defer func() {
		delete(state.active, p.ID)
		if err == nil {
			state.done[p.ID] = stale
		}
	}()
	if err := validatePlaylistBindings(ctx, tx, owner, p.Bindings); err != nil {
		return true, err
	}
	if p.SnapshotRevision == 0 {
		return true, nil
	}
	snapshot, err := readPersonalSnapshot(ctx, tx, owner, p.ID, p.SnapshotRevision)
	if err != nil {
		return true, err
	}
	if snapshot.RuleRevision != p.RuleRevision || snapshot.HideRevision != p.HideRevision || snapshot.MatchingRevision != matching {
		return true, nil
	}
	for _, dep := range snapshot.Dependencies {
		switch dep.Kind {
		case "remote":
			remote, err := readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, dep.ID))
			if err != nil {
				return true, err
			}
			if remote.Revision != dep.Revision || remote.Stale {
				return true, nil
			}
		case "playlist":
			child, err := readPersonalPlaylist(ctx, tx, owner, dep.ID)
			if err != nil {
				return true, err
			}
			if child.SnapshotRevision != dep.Revision {
				return true, nil
			}
			stale, err := personalPlaylistStale(ctx, tx, owner, child, matching, state, depth+1)
			if err != nil || stale {
				return stale, err
			}
		default:
			return true, playlists.Invalid()
		}
	}
	return false, nil
}
func (s *Store) Playlist(ctx context.Context, owner, id string) (playlists.Playlist, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return playlists.Playlist{}, err
	}
	defer tx.Rollback()
	p, err := readPersonalPlaylist(ctx, tx, owner, id)
	if err != nil {
		return p, err
	}
	matching, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		return p, err
	}
	p.Stale, err = personalPlaylistStale(ctx, tx, owner, p, matching, newPlaylistStaleState(), 1)
	return p, err
}
func (s *Store) Playlists(ctx context.Context, owner, after string, limit int) ([]playlists.Playlist, error) {
	return s.SearchPlaylists(ctx, owner, "", after, limit)
}

func (s *Store) SearchPlaylists(ctx context.Context, owner, query, after string, limit int) ([]playlists.Playlist, error) {
	query = strings.TrimSpace(query)
	if !utf8.ValidString(query) || len(query) > 256 || strings.ContainsFunc(query, unicode.IsControl) {
		return nil, playlists.Invalid()
	}
	if limit < 1 || limit > playlists.MaxPageSize+1 || after != "" && !playlists.ValidID(after) {
		return nil, playlists.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids, err := searchPersonalPlaylistIDs(ctx, tx, owner, query, after, limit)
	if err != nil {
		return nil, err
	}
	result := []playlists.Playlist{}
	matching, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		p, err := readPersonalPlaylist(ctx, tx, owner, id)
		if err != nil {
			return nil, err
		}
		p.Stale, err = personalPlaylistStale(ctx, tx, owner, p, matching, newPlaylistStaleState(), 1)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func searchPersonalPlaylistIDs(ctx context.Context, tx *sql.Tx, owner, query, after string, limit int) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,document FROM personal_playlists WHERE owner=? AND id>? ORDER BY id", owner, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	query = strings.ToLower(query)
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var p playlists.Playlist
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(p.Name), query) {
			ids = append(ids, id)
			if len(ids) == limit {
				break
			}
		}
	}
	return ids, rows.Err()
}
func (s *Store) PlaylistSnapshot(ctx context.Context, owner, id string, revision int64) (playlists.AggregateSnapshot, error) {
	var empty playlists.AggregateSnapshot
	if revision < 1 {
		return empty, playlists.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	snapshot, err := readPersonalSnapshot(ctx, tx, owner, id, revision)
	if err != nil {
		return empty, err
	}
	return snapshot, authorizePlaylistSnapshot(ctx, tx, owner, snapshot)
}
func (s *Store) PlaylistChanges(ctx context.Context, owner, id string, after int64, limit int) ([]playlists.PlaylistChange, error) {
	if after < 0 || limit < 1 || limit > playlists.MaxPageSize+1 {
		return nil, playlists.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = readPersonalPlaylist(ctx, tx, owner, id); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM playlist_changes WHERE owner=? AND playlist_id=? AND sequence>? ORDER BY sequence LIMIT ?", owner, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []playlists.PlaylistChange{}
	for rows.Next() {
		var raw []byte
		var c playlists.PlaylistChange
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

var _ playlists.AggregateRepository = (*Store)(nil)

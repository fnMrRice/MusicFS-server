package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type playlistHiddenRecord struct {
	Rule             playlists.HiddenRule
	Allowed          []playlists.SourceRef
	MatchingRevision int64
}

func readPlaylistHidden(ctx context.Context, tx *sql.Tx, owner, id string) ([]playlistHiddenRecord, error) {
	rows, err := tx.QueryContext(ctx, "SELECT document FROM playlist_hidden_rules WHERE owner=? AND playlist_id=? ORDER BY id LIMIT 1025", owner, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []playlistHiddenRecord{}
	for rows.Next() {
		var raw []byte
		var r playlistHiddenRecord
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if len(result) > 1024 {
		return nil, domain.Fail("RATE_LIMITED", "Playlist hidden-rule count exceeds its limit")
	}
	return result, rows.Err()
}
func (b *playlistBuilder) hiddenEligibility(r playlistHiddenRecord) (map[string]bool, bool, error) {
	group, err := b.group(r.Rule.Source)
	if err != nil {
		var known *domain.Error
		if errors.As(err, &known) && known.Code == "NOT_FOUND" {
			return map[string]bool{}, true, nil
		}
		return nil, false, err
	}
	allowed := map[string]bool{}
	for _, source := range r.Allowed {
		allowed[source.Key()] = true
	}
	eligible := map[string]bool{}
	confirmation := false
	for _, source := range group.members {
		if allowed[source.Key()] {
			eligible[source.Key()] = true
		} else {
			confirmation = true
		}
	}
	return eligible, confirmation, nil
}
func hiddenPathMatches(r playlists.HiddenRule, p playlists.Provenance) bool {
	if r.Scope == "allSources" {
		return true
	}
	if len(p.BindingPath) < len(r.BindingPath) {
		return false
	}
	for i, id := range r.BindingPath {
		if p.BindingPath[i] != id {
			return false
		}
	}
	return r.Scope == "binding" || r.EntryID != nil && p.EntryID == *r.EntryID && len(p.BindingPath) == len(r.BindingPath)
}
func (b *playlistBuilder) applyHidden(id string, items []playlists.Provenance) ([]string, error) {
	rules, err := readPlaylistHidden(b.ctx, b.tx, b.owner, id)
	if err != nil {
		return nil, err
	}
	confirmation := []string{}
	for _, r := range rules {
		if !r.Rule.Active {
			continue
		}
		eligible, confirm, err := b.hiddenEligibility(r)
		if err != nil {
			return nil, err
		}
		if confirm {
			confirmation = append(confirmation, r.Rule.ID)
		}
		for i := range items {
			if eligible[items[i].Source.Key()] && hiddenPathMatches(r.Rule, items[i]) {
				items[i].Hidden = true
			}
		}
	}
	return confirmation, nil
}
func hiddenProjection(ctx context.Context, tx *sql.Tx, owner string, p playlists.Playlist) (playlists.HiddenRules, error) {
	result := playlists.HiddenRules{PlaylistID: p.ID, Revision: p.HideRevision, Items: []playlists.HiddenRule{}}
	rules, err := readPlaylistHidden(ctx, tx, owner, p.ID)
	if err != nil {
		return result, err
	}
	b, err := newPlaylistBuilder(ctx, tx, owner)
	if err != nil {
		return result, err
	}
	for _, r := range rules {
		if r.Rule.Active {
			_, r.Rule.NeedsConfirmation, err = b.hiddenEligibility(r)
			if err != nil {
				return result, err
			}
		}
		result.Items = append(result.Items, r.Rule)
	}
	return result, nil
}
func (s *Store) HiddenRules(ctx context.Context, owner, id string) (playlists.HiddenRules, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return playlists.HiddenRules{}, err
	}
	defer tx.Rollback()
	p, err := readPersonalPlaylist(ctx, tx, owner, id)
	if err != nil {
		return playlists.HiddenRules{}, err
	}
	return hiddenProjection(ctx, tx, owner, p)
}
func savePlaylistHidden(ctx context.Context, tx *sql.Tx, owner, playlist string, r playlistHiddenRecord, insert bool) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO playlist_hidden_rules VALUES(?,?,?,?,?)", r.Rule.ID, owner, playlist, r.Rule.Active, raw)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE playlist_hidden_rules SET active=?,document=? WHERE owner=? AND playlist_id=? AND id=?", r.Rule.Active, raw, owner, playlist, r.Rule.ID)
	}
	return err
}
func (s *Store) HidePlaylistSource(ctx context.Context, owner, id string, r playlists.HideRequest, key string) (playlists.HiddenRules, error) {
	var empty playlists.HiddenRules
	if err := r.Validate(); err != nil {
		return empty, err
	}
	if r.BindingPath == nil {
		r.BindingPath = []string{}
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "hide/"+id, key, r)
	if err != nil {
		return empty, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &empty)
		return empty, err
	}
	p, err := readPersonalPlaylist(ctx, m.tx, owner, id)
	if err != nil {
		return empty, err
	}
	if p.HideRevision != r.ExpectedRevision {
		return empty, domain.Fail("CONFLICT", "Playlist hidden rules changed")
	}
	rules, err := readPlaylistHidden(ctx, m.tx, owner, id)
	if err != nil {
		return empty, err
	}
	if len(rules) >= 1024 {
		return empty, domain.Fail("RATE_LIMITED", "Playlist hidden-rule limit reached")
	}
	b, err := newPlaylistBuilder(ctx, m.tx, owner)
	if err != nil {
		return empty, err
	}
	snapshot, err := b.build(id, 1)
	if err != nil {
		return empty, err
	}
	now := time.Now().UTC()
	rule := playlists.HiddenRule{ID: domain.NewID(), Scope: r.Scope, Source: r.Source, BindingPath: r.BindingPath, EntryID: r.EntryID, Active: true, CreatedAt: now, UpdatedAt: now}
	found := false
	for _, entry := range snapshot.Entries {
		for _, provenance := range entry.Provenance {
			if provenance.Source.Key() == r.Source.Key() && hiddenPathMatches(rule, provenance) {
				found = true
			}
		}
	}
	if !found {
		return empty, domain.Fail("NOT_FOUND", "Playlist contribution not found")
	}
	group, err := b.group(r.Source)
	if err != nil {
		return empty, err
	}
	record := playlistHiddenRecord{Rule: rule, Allowed: group.members, MatchingRevision: b.matching}
	if err = savePlaylistHidden(ctx, m.tx, owner, id, record, true); err != nil {
		return empty, err
	}
	// build may have advanced this head inside the same transaction.
	p, err = readPersonalPlaylist(ctx, m.tx, owner, id)
	if err != nil {
		return empty, err
	}
	p.HideRevision++
	p.Stale = true
	if err = savePersonalPlaylist(ctx, m.tx, owner, &p, false); err != nil {
		return empty, err
	}
	if err = playlistAudit(ctx, m.tx, owner, p, "hide", &rule.ID); err != nil {
		return empty, err
	}
	result, err := hiddenProjection(ctx, m.tx, owner, p)
	if err != nil {
		return empty, err
	}
	return result, m.finish(ctx, result)
}
func (s *Store) ChangePlaylistHidden(ctx context.Context, owner, id, ruleID string, r playlists.ChangeHiddenRequest, key string) (playlists.HiddenRules, error) {
	var empty playlists.HiddenRules
	if r.ExpectedRevision < 0 || !playlists.ValidID(ruleID) || r.Confirm && !r.Active {
		return empty, playlists.Invalid()
	}
	m, err := s.beginPlaylistMutation(ctx, owner, "hidden/"+id+"/"+ruleID, key, r)
	if err != nil {
		return empty, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &empty)
		return empty, err
	}
	p, err := readPersonalPlaylist(ctx, m.tx, owner, id)
	if err != nil {
		return empty, err
	}
	if p.HideRevision != r.ExpectedRevision {
		return empty, domain.Fail("CONFLICT", "Playlist hidden rules changed")
	}
	rules, err := readPlaylistHidden(ctx, m.tx, owner, id)
	if err != nil {
		return empty, err
	}
	var record *playlistHiddenRecord
	for i := range rules {
		if rules[i].Rule.ID == ruleID {
			record = &rules[i]
			break
		}
	}
	if record == nil {
		return empty, domain.Fail("NOT_FOUND", "Playlist hidden rule not found")
	}
	if r.Confirm {
		_, members, e := playlistSourceGroup(ctx, m.tx, owner, record.Rule.Source)
		if e != nil {
			return empty, e
		}
		record.Allowed = members
		record.MatchingRevision, err = matchingRevision(ctx, m.tx, owner)
		if err != nil {
			return empty, err
		}
	}
	record.Rule.Active = r.Active
	record.Rule.UpdatedAt = time.Now().UTC()
	if err = savePlaylistHidden(ctx, m.tx, owner, id, *record, false); err != nil {
		return empty, err
	}
	p.HideRevision++
	p.Stale = true
	if err = savePersonalPlaylist(ctx, m.tx, owner, &p, false); err != nil {
		return empty, err
	}
	if err = playlistAudit(ctx, m.tx, owner, p, "changeHidden", &ruleID); err != nil {
		return empty, err
	}
	result, err := hiddenProjection(ctx, m.tx, owner, p)
	if err != nil {
		return empty, err
	}
	return result, m.finish(ctx, result)
}

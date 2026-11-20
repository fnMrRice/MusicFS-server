package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func matchingRevision(ctx context.Context, tx *sql.Tx, owner string) (int64, error) {
	var revision int64
	err := tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT revision FROM matching_revisions WHERE owner=?),0)", owner).Scan(&revision)
	return revision, err
}

func (s *Store) MatchingRevision(ctx context.Context, owner string) (int64, error) {
	var revision int64
	err := s.DB.QueryRowContext(ctx, "SELECT COALESCE((SELECT revision FROM matching_revisions WHERE owner=?),0)", owner).Scan(&revision)
	return revision, err
}

func sourceIDs(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > matching.MaxSources {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid source selection")
	}
	result := append([]string(nil), ids...)
	sort.Strings(result)
	for i, id := range result {
		if id == "" || len(id) > 128 || i > 0 && result[i-1] == id {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid or repeated source selection")
		}
	}
	return result, nil
}

func visibleSources(ctx context.Context, tx *sql.Tx, owner string, ids []string) error {
	for _, id := range ids {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM tracks WHERE owner=? AND id=?", owner, id).Scan(&count); err != nil {
			return err
		} else if count != 1 {
			return domain.Fail("NOT_FOUND", "Source track not found")
		}
	}
	return nil
}

type matchingMutation struct {
	tx                        *sql.Tx
	owner, route, key, digest string
	result                    matching.Mutation
	replay                    []byte
}

func (s *Store) beginMatching(ctx context.Context, owner, operation, key string, expected int64, request any) (*matchingMutation, bool, error) {
	if owner == "" || expected < 0 || len(key) < 8 || len(key) > 128 {
		return nil, false, domain.Fail("INVALID_ARGUMENT", "Invalid matching mutation")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, false, err
	}
	sum := sha256.Sum256(raw)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	m := &matchingMutation{tx: tx, owner: owner, route: "matching/" + operation, key: key, digest: hex.EncodeToString(sum[:])}
	var digest, expires string
	var result []byte
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, m.route, key).Scan(&digest, &result, &expires)
	if err == nil {
		if digest != m.digest {
			tx.Rollback()
			return nil, false, domain.Fail("CONFLICT", "Idempotency key was used with another matching decision")
		}
		until, e := time.Parse(time.RFC3339Nano, expires)
		if e != nil || time.Now().After(until) {
			tx.Rollback()
			return nil, false, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new operation")
		}
		if err = json.Unmarshal(result, &m.result); err != nil {
			tx.Rollback()
			return nil, false, err
		}
		for i := range m.result.Changes {
			m.result.Changes[i].Owner = owner
		}
		m.replay = result
		return m, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return nil, false, err
	}
	revision, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		tx.Rollback()
		return nil, false, err
	}
	if revision != expected {
		tx.Rollback()
		return nil, false, domain.Fail("CONFLICT", "Matching revision changed; refresh before applying the decision")
	}
	m.result = matching.Mutation{Revision: revision + 1, OperationID: domain.NewID(), Changes: []matching.LinkChange{}}
	return m, false, nil
}

func (m *matchingMutation) finish(ctx context.Context) (matching.Mutation, error) {
	return m.finishResponse(ctx, nil)
}

// finishResponse 共用审计与提交逻辑，并允许批量操作保存完整的幂等响应。
func (m *matchingMutation) finishResponse(ctx context.Context, response func(matching.Mutation) any) (matching.Mutation, error) {
	if err := m.saveChanges(ctx); err != nil {
		return m.result, err
	}
	var value any = m.result
	if response != nil {
		value = response(m.result)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return m.result, err
	}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO idempotency(owner,route,key,digest,result,expires_at) VALUES(?,?,?,?,?,?)", m.owner, m.route, m.key, m.digest, raw, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return m.result, err
	}
	return m.result, m.tx.Commit()
}

// saveChanges 在调用方事务内保存录音修订和逐来源的关联审计。
func (m *matchingMutation) saveChanges(ctx context.Context) error {
	if _, err := m.tx.ExecContext(ctx, "INSERT INTO matching_revisions(owner,revision) VALUES(?,?) ON CONFLICT(owner) DO UPDATE SET revision=excluded.revision", m.owner, m.result.Revision); err != nil {
		return err
	}
	var sequence int64
	if err := m.tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence),0) FROM link_changes WHERE owner=?", m.owner).Scan(&sequence); err != nil {
		return err
	}
	for i := range m.result.Changes {
		sequence++
		c := &m.result.Changes[i]
		c.Sequence, c.Owner, c.OperationID, c.Revision, c.CreatedAt = sequence, m.owner, m.result.OperationID, m.result.Revision, time.Now().UTC()
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err = m.tx.ExecContext(ctx, "INSERT INTO link_changes(sequence,owner,revision,operation_id,document) VALUES(?,?,?,?,?)", sequence, m.owner, m.result.Revision, m.result.OperationID, raw); err != nil {
			return err
		}
	}
	return nil
}

func strptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func linkRecording(ctx context.Context, tx *sql.Tx, owner, source string) (string, error) {
	var id sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT recording_id FROM recording_source_links WHERE owner=? AND source_id=?", owner, source).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id.String, err
}

func checkExclusions(ctx context.Context, tx *sql.Tx, owner string, ids []string) error {
	args := []any{owner}
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	var count int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM match_exclusions WHERE owner=? AND excluded=1 AND left_source IN ("+placeholders+") AND right_source IN ("+placeholders+")", args...).Scan(&count)
	if err == nil && count != 0 {
		return domain.Fail("CONFLICT", "The recording would contain an excluded pair; remove the exclusion first")
	}
	return err
}

func refreshManualCandidateStates(ctx context.Context, tx *sql.Tx, owner string, ids []string) error {
	args := []any{owner}
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	_, err := tx.ExecContext(ctx, `UPDATE matching_candidates AS c SET status=CASE
 WHEN EXISTS(SELECT 1 FROM match_exclusions e WHERE e.owner=c.owner AND e.left_source=c.left_source AND e.right_source=c.right_source AND e.excluded=1) THEN 'excluded'
 WHEN EXISTS(SELECT 1 FROM recording_source_links l JOIN recording_source_links r ON r.owner=l.owner AND r.recording_id=l.recording_id WHERE l.owner=c.owner AND l.source_id=c.left_source AND r.source_id=c.right_source AND l.recording_id IS NOT NULL) THEN 'linked'
 ELSE 'stale' END WHERE owner=? AND status!='stale' AND (left_source IN (`+marks+`) OR right_source IN (`+marks+`))`, args...)
	return err
}

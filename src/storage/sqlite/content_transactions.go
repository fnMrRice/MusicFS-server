package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

type contentMutation struct {
	tx                                *sql.Tx
	owner, source, route, key, digest string
	result                            mediastore.Selection
}

func (s *Store) beginContentMutation(ctx context.Context, owner, source, operation, key string, expected int64, request any) (*contentMutation, bool, error) {
	if owner == "" || source == "" || expected < 0 || len(key) < 8 || len(key) > 128 {
		return nil, false, domain.Fail("INVALID_ARGUMENT", "Invalid media selection operation")
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
	m := &contentMutation{tx: tx, owner: owner, source: source, route: "media/" + operation, key: key, digest: hex.EncodeToString(sum[:])}
	if err = visibleSources(ctx, tx, owner, []string{source}); err != nil {
		tx.Rollback()
		return nil, false, err
	}
	prior, exists, err := contentIdempotency(ctx, tx, owner, m.route, key, m.digest)
	if err != nil {
		tx.Rollback()
		return nil, false, err
	}
	if exists {
		if err = json.Unmarshal(prior, &m.result); err != nil {
			tx.Rollback()
			return nil, false, err
		}
		return m, true, nil
	}
	if m.result, err = readContentSelection(ctx, tx, owner, source); err != nil {
		tx.Rollback()
		return nil, false, err
	}
	if m.result.Revision != expected {
		tx.Rollback()
		return nil, false, domain.Fail("CONFLICT", "Media selection revision changed")
	}
	return m, false, nil
}

func (m *contentMutation) finish(ctx context.Context, operation, field, language, old, next string, locked bool) (mediastore.Selection, error) {
	var empty mediastore.Selection
	revision, err := advanceContentRevision(ctx, m.tx, m.owner, m.source)
	if err != nil {
		return empty, err
	}
	if err = recordContentChange(ctx, m.tx, m.owner, m.source, revision, operation, field, language, old, next, locked); err != nil {
		return empty, err
	}
	result, err := readContentSelection(ctx, m.tx, m.owner, m.source)
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	if err = saveContentIdempotency(ctx, m.tx, m.owner, m.route, m.key, m.digest, raw); err != nil {
		return empty, err
	}
	return result, m.tx.Commit()
}

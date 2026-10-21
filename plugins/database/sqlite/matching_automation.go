package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func (s *Store) MatchingOwners(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT owner FROM tracks UNION SELECT owner FROM matching_runs ORDER BY owner")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := []string{}
	for rows.Next() {
		var owner string
		if err = rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}
func (s *Store) MatchingLibrary(ctx context.Context, owner string) (matching.LibrarySnapshot, error) {
	result := matching.LibrarySnapshot{Sources: []matching.CandidateSource{}}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM tracks WHERE owner=? AND json_extract(document,'$.availability')='available' ORDER BY id", owner)
	if err != nil {
		return result, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	h := sha256.New()
	manualRevision, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		return result, err
	}
	fmt.Fprintf(h, "manual:%d\n", manualRevision)
	for _, id := range ids {
		source, e := matchingSource(ctx, tx, owner, id)
		if e != nil {
			var failure *domain.Error
			if errors.As(e, &failure) && failure.Code == "RESOURCE_CHANGED" {
				continue
			}
			return result, e
		}
		result.Sources = append(result.Sources, source)
		h.Write([]byte(id + ":" + source.BindingRevision + "\n"))
	}
	result.Revision = hex.EncodeToString(h.Sum(nil))
	return result, nil
}
func (s *Store) MatchingRun(ctx context.Context, owner string) (matching.Run, error) {
	r := matching.Run{Owner: owner, Job: settings.Job{Status: "idle"}}
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT document FROM matching_runs WHERE owner=?", owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r)
	return r, err
}
func (s *Store) SaveMatchingRun(ctx context.Context, r matching.Run) error {
	if r.Owner == "" {
		return domain.Fail("INVALID_ARGUMENT", "Matching owner is required")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO matching_runs(owner,document) VALUES(?,?) ON CONFLICT(owner) DO UPDATE SET document=excluded.document", r.Owner, raw)
	if err != nil {
		return err
	}
	if r.Key != "" {
		job, e := json.Marshal(r.Job)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO matching_action_receipts(owner,idempotency_key,document) VALUES(?,?,?) ON CONFLICT(owner,idempotency_key) DO UPDATE SET document=excluded.document", r.Owner, r.Key, job); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MatchingAction(ctx context.Context, owner, key string) (*settings.Job, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT document FROM matching_action_receipts WHERE owner=? AND idempotency_key=?", owner, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var job settings.Job
	err = json.Unmarshal(raw, &job)
	return &job, err
}

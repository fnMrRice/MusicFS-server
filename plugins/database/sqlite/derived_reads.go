package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

// Historical assets remain readable, but a copied owner field never substitutes
// for the underlying source authorization. A complete document includes all job
// targets, so each of those sources must still belong to this owner.
const derivedAuthorization = ` ((d.transform='thumbnail256/v1' AND EXISTS(
 SELECT 1 FROM media_content_objects c JOIN tracks t ON t.id=c.source_id AND t.owner=c.owner
 WHERE c.id=d.parent_object_id AND c.owner=d.owner)) OR
 (d.transform='scrape-document/v1' AND EXISTS(
 SELECT 1 FROM enrichment_jobs j JOIN enrichment_targets e ON e.job_id=j.id AND e.owner=j.owner
 JOIN tracks t ON t.id=e.source_id AND t.owner=e.owner
 WHERE j.id=d.job_id AND j.owner=d.owner AND e.source_id=d.source_id)
 AND NOT EXISTS(SELECT 1 FROM enrichment_targets e LEFT JOIN tracks t ON t.id=e.source_id
 WHERE e.job_id=d.job_id AND (e.owner!=d.owner OR t.id IS NULL OR t.owner!=d.owner))))`

func readDerived(row rowScanner) (mediastore.DerivedObject, error) {
	var o mediastore.DerivedObject
	var raw []byte
	var state string
	err := row.Scan(&raw, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return o, domain.Fail("NOT_FOUND", "Derived media object not found")
	}
	if err != nil {
		return o, err
	}
	if err = json.Unmarshal(raw, &o); err != nil {
		return o, err
	}
	o.State = state
	o.RelativePath = "objects/" + o.ID
	return o, o.Object.Validate()
}

func (s *Store) DerivedObject(ctx context.Context, owner, id string) (mediastore.DerivedObject, error) {
	return readDerived(s.DB.QueryRowContext(ctx, "SELECT d.document,d.state FROM media_derived_objects d WHERE d.owner=? AND d.id=? AND "+derivedAuthorization, owner, id))
}
func (s *Store) FindDerived(ctx context.Context, owner, key string) (mediastore.DerivedObject, error) {
	return readDerived(s.DB.QueryRowContext(ctx, "SELECT d.document,d.state FROM media_derived_objects d WHERE d.owner=? AND d.relationship_key=? AND d.state='available' AND "+derivedAuthorization, owner, key))
}
func (s *Store) ListScrapeDocuments(ctx context.Context, owner, job string) ([]mediastore.DerivedObject, error) {
	var exists int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM enrichment_jobs WHERE owner=? AND id=?", owner, job).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, domain.Fail("NOT_FOUND", "Metadata job not found")
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT d.document,d.state FROM media_derived_objects d WHERE d.owner=? AND d.job_id=? AND "+derivedAuthorization+" ORDER BY d.source_id,d.generation,d.id LIMIT 1025", owner, job)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mediastore.DerivedObject{}
	for rows.Next() {
		o, err := readDerived(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	if len(items) > 1024 {
		return nil, domain.Fail("RATE_LIMITED", "Scrape document listing exceeds its limit")
	}
	return items, rows.Err()
}
func (s *Store) InvalidateDerived(ctx context.Context, owner, id string) error {
	var publication string
	err := s.DB.QueryRowContext(ctx, "SELECT d.publication_id FROM media_derived_objects d WHERE d.owner=? AND d.id=? AND "+derivedAuthorization, owner, id).Scan(&publication)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Derived media object not found")
	}
	if err != nil {
		return err
	}
	return s.InvalidatePublication(ctx, owner, publication, "RESOURCE_CHANGED")
}

var _ mediastore.DerivedRepository = (*Store)(nil)

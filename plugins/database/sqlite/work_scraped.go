package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func (s *Store) WorkScrapedFields(ctx context.Context, owner, id string) ([]works.ScrapedField, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var found bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM works WHERE owner=? AND id=?)", owner, id).Scan(&found); err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.Fail("NOT_FOUND", "Work not found")
	}
	rows, err := tx.QueryContext(ctx, "SELECT f.value FROM works w, json_each(w.document,'$.scrapedFields') f WHERE w.owner=? AND w.id=? ORDER BY f.key", owner, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := []works.ScrapedField{}
	for rows.Next() {
		var raw []byte
		var field works.ScrapedField
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &field); err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, rows.Err()
}

func (s *Store) SelectWorkScrapedField(ctx context.Context, owner, id string, revision int64, field works.ScrapedField, key string) (int64, error) {
	if !works.ValidID(id) || !scraper.ValidField(field.Field) || revision < 1 || !works.ValidID(field.PluginID) || !works.ValidID(field.CandidateToken) {
		return 0, domain.Fail("INVALID_ARGUMENT", "Invalid scraped field selection")
	}
	m, err := s.beginWorkMutation(ctx, owner, id+"/scraped/"+field.Field, key, struct {
		Revision int64
		PluginID string
		Token    string
	}{revision, field.PluginID, field.CandidateToken})
	if err != nil {
		return 0, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		var prior works.Work
		err = json.Unmarshal(m.prior, &prior)
		return prior.Revision, err
	}
	if field.Item.ID == "" {
		return 0, domain.Fail("NOT_FOUND", "Scraper candidate expired or unavailable")
	}
	work, err := readWork(ctx, m.tx, owner, id)
	if err != nil {
		return 0, err
	}
	if work.Revision != revision {
		return 0, domain.Fail("CONFLICT", "Work revision changed")
	}
	field.CandidateToken = ""
	raw, err := json.Marshal(field)
	if err != nil {
		return 0, err
	}
	work.Revision++
	work.UpdatedAt = time.Now().UTC()
	changed, err := m.tx.ExecContext(ctx, `UPDATE works SET revision=?,document=json_set(document,'$.revision',?,'$.updatedAt',?,?,json(?)) WHERE owner=? AND id=? AND revision=?`, work.Revision, work.Revision, work.UpdatedAt.Format(time.RFC3339Nano), "$.scrapedFields."+field.Field, string(raw), owner, id, revision)
	if err != nil {
		return 0, err
	}
	n, err := changed.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, domain.Fail("CONFLICT", "Work revision changed")
	}
	return work.Revision, m.finish(ctx, work)
}

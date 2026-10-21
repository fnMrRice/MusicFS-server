package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func (s *Store) ScanAutomationResults(ctx context.Context, owner, scan, cursor string, limit int) (enrichment.AutomationPage, error) {
	p := enrichment.AutomationPage{Items: []enrichment.AutomationResult{}}
	if limit < 1 || limit > 200 {
		return p, domain.Fail("INVALID_ARGUMENT", "limit must be between 1 and 200")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM jobs WHERE owner=? AND id=?", owner, scan).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return p, domain.Fail("NOT_FOUND", "Scan job not found")
	}
	if err != nil {
		return p, err
	}
	if cursor != "" {
		err = tx.QueryRowContext(ctx, "SELECT 1 FROM metadata_automation WHERE owner=? AND scan_job_id=? AND id=?", owner, scan, cursor).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return p, domain.Fail("CURSOR_EXPIRED", "Cursor does not match the metadata results")
		}
		if err != nil {
			return p, err
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+automationColumns+" FROM metadata_automation a WHERE owner=? AND scan_job_id=? AND id>? ORDER BY id LIMIT ?", owner, scan, cursor, limit+1)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		w, e := readAutomation(rows)
		if e != nil {
			rows.Close()
			return p, e
		}
		p.Items = append(p.Items, w.AutomationResult)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	if len(p.Items) > limit {
		p.Items = p.Items[:limit]
		next := p.Items[limit-1].TriggerID
		p.NextCursor = &next
	}
	for i := range p.Items {
		rows, err = tx.QueryContext(ctx, "SELECT document FROM metadata_automation_actions WHERE trigger_id=? ORDER BY field,language", p.Items[i].TriggerID)
		if err != nil {
			return p, err
		}
		for rows.Next() {
			var raw []byte
			var a enrichment.AutomationAction
			if err = rows.Scan(&raw); err == nil {
				err = json.Unmarshal(raw, &a)
			}
			if err != nil {
				rows.Close()
				return p, err
			}
			p.Items[i].Actions = append(p.Items[i].Actions, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

var _ works.Repository = (*Store)(nil)

func readWork(ctx context.Context, tx *sql.Tx, owner, id string) (works.Work, error) {
	var result works.Work
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM works WHERE owner=? AND id=?", owner, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.Fail("NOT_FOUND", "Work not found")
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
		result.Owner = owner
	}
	return result, err
}

func (s *Store) Work(ctx context.Context, owner, id string) (works.Work, error) {
	if !works.ValidID(owner) || !works.ValidID(id) {
		return works.Work{}, domain.Fail("INVALID_ARGUMENT", "Invalid work identifier")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return works.Work{}, err
	}
	defer tx.Rollback()
	return readWork(ctx, tx, owner, id)
}

// Works 使用稳定的作品 ID 游标，筛选条件按交集应用；搜索参数始终绑定。
func (s *Store) Works(ctx context.Context, owner string, query works.Query) ([]works.Work, error) {
	if !works.ValidID(owner) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid work owner")
	}
	query, err := works.NormalizeQuery(query)
	if err != nil {
		return nil, err
	}
	clauses := []string{"w.owner=?", "w.id>?"}
	args := []any{owner, query.After}
	if query.ContentType != "" {
		clauses, args = append(clauses, "w.content_type=?"), append(args, query.ContentType)
	}
	if query.PackageKind != "" {
		clauses = append(clauses, "EXISTS(SELECT 1 FROM work_packages p WHERE p.owner=w.owner AND p.work_id=w.id AND p.kind=?)")
		args = append(args, query.PackageKind)
	}
	if query.Q != "" {
		clauses = append(clauses, `(instr(lower(w.title),lower(?))>0 OR instr(lower(w.description),lower(?))>0
		 OR EXISTS(SELECT 1 FROM work_external_ids e WHERE e.work_id=w.id AND (instr(lower(e.value),lower(?))>0 OR instr(lower(e.namespace),lower(?))>0))
		 OR EXISTS(SELECT 1 FROM work_credits c WHERE c.work_id=w.id AND instr(lower(c.name),lower(?))>0))`)
		args = append(args, query.Q, query.Q, query.Q, query.Q, query.Q)
	}
	if query.Tag != "" {
		clauses = append(clauses, "EXISTS(SELECT 1 FROM work_tags t WHERE t.work_id=w.id AND t.tag=?)")
		args = append(args, query.Tag)
	}
	if query.Credit != "" {
		clauses = append(clauses, "EXISTS(SELECT 1 FROM work_credits c WHERE c.work_id=w.id AND c.name=?)")
		args = append(args, query.Credit)
	}
	args = append(args, query.Limit)
	rows, err := s.DB.QueryContext(ctx, "SELECT w.document FROM works w WHERE "+strings.Join(clauses, " AND ")+" ORDER BY w.id LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []works.Work{}
	for rows.Next() {
		var raw []byte
		var item works.Work
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		item.Owner = owner
		result = append(result, item)
	}
	return result, rows.Err()
}

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

// ReadRecommendationPage 在同一数据库快照中读取内容和可回看的次数。
func (s *Store) ReadRecommendationPage(ctx context.Context, owner, scope string, history int) (recommendations.SavedPage, error) {
	var page recommendations.SavedPage
	if history < 0 || history > recommendations.MaxHistory {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid recommendation history position")
	}
	var data string
	err := s.DB.QueryRowContext(ctx, `
		SELECT server_day, sources_json, (
			SELECT max(0, count(*) - 1) FROM recommendation_pages
			WHERE owner_id = ?1 AND scope = ?2 AND successful = 1
		)
		FROM recommendation_pages
		WHERE owner_id = ?1 AND scope = ?2 AND (?3 = 0 OR successful = 1)
		ORDER BY id DESC LIMIT 1 OFFSET ?3`, owner, scope, history).Scan(&page.Day, &data, &page.HistoryDepth)
	if errors.Is(err, sql.ErrNoRows) {
		return page, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(data), &page.Sources)
	}
	return page, err
}

// SaveRecommendationPage 原子发布本次尝试。失败只更新当前页的状态，不占历史名额。
func (s *Store) SaveRecommendationPage(ctx context.Context, owner, scope, day string, sources []recommendations.Source, successful bool) (int, error) {
	data, err := json.Marshal(sources)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM recommendation_pages
		WHERE owner_id = ? AND scope = ? AND successful = 0`, owner, scope); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO recommendation_pages
		(owner_id, scope, server_day, successful, sources_json) VALUES (?, ?, ?, ?, ?)`, owner, scope, day, successful, string(data)); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recommendation_pages WHERE id IN (
		SELECT id FROM recommendation_pages WHERE owner_id = ? AND scope = ? AND successful = 1
		ORDER BY id DESC LIMIT -1 OFFSET ?)`, owner, scope, recommendations.MaxHistory+1); err != nil {
		return 0, err
	}
	// 淘汰长期闲置的账号、来源组合，避免旧登录代次无限占用磁盘。
	if _, err = tx.ExecContext(ctx, `DELETE FROM recommendation_pages WHERE owner_id = ?1 AND scope IN (
		SELECT scope FROM recommendation_pages WHERE owner_id = ?1 GROUP BY scope ORDER BY max(id) DESC LIMIT -1 OFFSET 64)`, owner); err != nil {
		return 0, err
	}
	var depth int
	if err = tx.QueryRowContext(ctx, `SELECT max(0, count(*) - 1) FROM recommendation_pages
		WHERE owner_id = ? AND scope = ? AND successful = 1`, owner, scope).Scan(&depth); err != nil {
		return 0, err
	}
	return depth, tx.Commit()
}

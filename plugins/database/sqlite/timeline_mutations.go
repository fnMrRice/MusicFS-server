package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
)

func timelineReplay(ctx context.Context, tx *sql.Tx, owner, route, key string, request any) (string, *timeline.Evidence, error) {
	if owner == "" || len(key) < 8 || len(key) > 128 {
		return "", nil, domain.Fail("INVALID_ARGUMENT", "Invalid timeline mutation")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	var old, expiry string
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&old, &raw, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return digest, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if old != digest {
		return "", nil, domain.Fail("CONFLICT", "Idempotency key has different timeline parameters")
	}
	until, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !time.Now().Before(until) {
		return "", nil, domain.Fail("IDEMPOTENCY_EXPIRED", "Timeline operation expired")
	}
	var evidence timeline.Evidence
	err = json.Unmarshal(raw, &evidence)
	return digest, &evidence, err
}
func saveTimeline(ctx context.Context, tx *sql.Tx, owner, route, key, digest string, e timeline.Evidence) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO timeline_evidence VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,revoked_at=excluded.revoked_at,document=excluded.document", e.ID, owner, e.RecordingID, e.From.SourceTrackID, e.To.SourceTrackID, e.Revision, e.RevokedAt, raw, e.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", owner, route, key, digest, raw, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) CreateTimeline(ctx context.Context, owner string, r timeline.Request, key string) (timeline.Evidence, error) {
	var empty timeline.Evidence
	if err := r.Validate(); err != nil {
		return empty, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	digest, prior, err := timelineReplay(ctx, tx, owner, "timeline/create", key, r)
	if err != nil {
		return empty, err
	}
	if prior != nil {
		return *prior, nil
	}
	a, b, recording, err := timelinePair(ctx, tx, owner, r.From, r.To)
	if err != nil {
		return empty, err
	}
	if recording != r.RecordingID {
		return empty, domain.Fail("CONFLICT", "Recording association changed")
	}
	if a.DurationMs == nil || b.DurationMs == nil || r.Range.EndMs > *a.DurationMs || r.Range.EndMs+r.OffsetMs > *b.DurationMs {
		return empty, domain.Fail("INVALID_ARGUMENT", "Alignment exceeds verified source durations")
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM timeline_evidence WHERE owner=? AND revoked_at IS NULL", owner).Scan(&count); err != nil {
		return empty, err
	}
	if count >= 10000 {
		return empty, domain.Fail("RATE_LIMITED", "Active timeline evidence limit reached")
	}
	e := timeline.Evidence{ID: domain.NewID(), Request: r, Origin: "manualAlignment", Revision: 1, CreatedAt: time.Now().UTC()}
	return e, saveTimeline(ctx, tx, owner, "timeline/create", key, digest, e)
}
func (s *Store) RevokeTimeline(ctx context.Context, owner, id string, expected int64, key string) (timeline.Evidence, error) {
	var empty timeline.Evidence
	if id == "" || expected < 1 {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid timeline revocation")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	route := "timeline/" + id + "/revoke"
	digest, prior, err := timelineReplay(ctx, tx, owner, route, key, expected)
	if err != nil {
		return empty, err
	}
	if prior != nil {
		return *prior, nil
	}
	e, err := readTimeline(ctx, tx, owner, id)
	if err != nil {
		return empty, err
	}
	if e.Revision != expected {
		return empty, domain.Fail("CONFLICT", "Timeline evidence revision changed")
	}
	if e.RevokedAt == nil {
		now := time.Now().UTC()
		e.RevokedAt = &now
		e.Revision++
	}
	return e, saveTimeline(ctx, tx, owner, route, key, digest, e)
}

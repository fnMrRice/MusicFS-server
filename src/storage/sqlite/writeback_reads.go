package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

func normalizeWriteback(r writebackhost.Request) (writebackhost.Request, error) {
	if r.SourceTrackID == "" || r.AssetID == "" || r.AssetVersion == "" || r.ExpectedRevision < 0 || len(r.Fields) < 1 || len(r.Fields) > 2 {
		return r, domain.Fail("INVALID_ARGUMENT", "Invalid writeback request")
	}
	for _, v := range []string{r.SourceTrackID, r.AssetID, r.AssetVersion} {
		if len(v) > 1024 || strings.ContainsAny(v, "\x00\r\n") {
			return r, domain.Fail("INVALID_ARGUMENT", "Invalid writeback identity")
		}
	}
	r.Fields = append([]writebackhost.Field{}, r.Fields...)
	for i := range r.Fields {
		f := &r.Fields[i]
		f.Language = mediastore.CanonicalLanguage(f.Name, f.Language)
		format := "text"
		if f.Name == "artwork" {
			format = "png"
		}
		m := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: r.SourceTrackID, AssetID: r.AssetID, AssetVersion: r.AssetVersion}, Field: f.Name, Language: f.Language, Format: format, Origin: mediastore.Origin{Kind: "upload"}}
		if err := m.Validate(); err != nil {
			return r, err
		}
	}
	slices.SortFunc(r.Fields, func(a, b writebackhost.Field) int { return strings.Compare(a.Name, b.Name) })
	if len(r.Fields) == 2 && r.Fields[0].Name == r.Fields[1].Name {
		return r, domain.Fail("INVALID_ARGUMENT", "Writeback supports one selected language per lyrics field")
	}
	return r, nil
}

func writebackDigest(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validWritebackTarget(r writebackhost.Request, t writeback.Target) bool {
	return t.SourceTrackID == r.SourceTrackID && t.AssetID == r.AssetID && t.AssetVersion == r.AssetVersion && t.InstanceID != "" && t.RootID != "" && t.FileIdentity != "" && len(t.FileIdentity) <= 1024 && fs.ValidPath(t.RelativePath) && t.RelativePath != "." && !strings.ContainsAny(t.RelativePath, "\\:\x00\r\n")
}

const writebackColumns = "work,state,error,result,updated_at,owner,old_size"

func readWriteback(row rowScanner) (writebackhost.Work, int64, error) {
	var w writebackhost.Work
	var size int64
	var raw, failure, result []byte
	var state, updated, owner string
	err := row.Scan(&raw, &state, &failure, &result, &updated, &owner, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return w, size, domain.Fail("NOT_FOUND", "Writeback job not found")
	}
	if err != nil {
		return w, size, err
	}
	if err = json.Unmarshal(raw, &w); err != nil {
		return w, size, err
	}
	w.Job.Owner, w.Job.Status = owner, state
	if w.Job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return w, size, err
	}
	if len(failure) > 0 {
		if err = json.Unmarshal(failure, &w.Job.Error); err != nil {
			return w, size, err
		}
	}
	if len(result) > 0 {
		err = json.Unmarshal(result, &w.Job.Result)
	}
	return w, size, err
}

func (s *Store) GetWriteback(ctx context.Context, owner, id string) (writebackhost.Job, error) {
	w, err := s.WritebackWork(ctx, owner, id)
	return w.Job, err
}
func (s *Store) WritebackWork(ctx context.Context, owner, id string) (writebackhost.Work, error) {
	w, _, err := readWriteback(s.DB.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	return w, err
}
func (s *Store) NextWriteback(ctx context.Context) (writebackhost.Work, error) {
	var id, owner string
	if err := s.DB.QueryRowContext(ctx, "SELECT id,owner FROM writeback_jobs WHERE state='queued' ORDER BY updated_at,id LIMIT 1").Scan(&id, &owner); err != nil {
		return writebackhost.Work{}, err
	}
	return s.WritebackWork(ctx, owner, id)
}

func replayWriteback(ctx context.Context, tx *sql.Tx, owner string, r writebackhost.Request, key string) (writebackhost.Job, bool, string, error) {
	var empty writebackhost.Job
	if owner == "" || len(key) < 8 || len(key) > 128 {
		return empty, false, "", domain.Fail("INVALID_ARGUMENT", "Invalid writeback operation")
	}
	digest, err := writebackDigest(r)
	if err != nil {
		return empty, false, "", err
	}
	var prior, binding, id, expires string
	err = tx.QueryRowContext(ctx, "SELECT request_digest,binding_digest,job_id,expires_at FROM writeback_operations WHERE owner=? AND key=?", owner, key).Scan(&prior, &binding, &id, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, false, "", nil
	}
	if err != nil {
		return empty, false, "", err
	}
	if prior != digest {
		return empty, false, "", domain.Fail("CONFLICT", "Writeback key was used for a different request")
	}
	until, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().After(until) {
		return empty, false, "", domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new writeback operation")
	}
	w, _, err := readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	return w.Job, true, binding, err
}

func (s *Store) ReplayWriteback(ctx context.Context, owner string, r writebackhost.Request, key string) (writebackhost.Job, bool, error) {
	r, err := normalizeWriteback(r)
	if err != nil {
		return writebackhost.Job{}, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return writebackhost.Job{}, false, err
	}
	defer tx.Rollback()
	j, replay, _, err := replayWriteback(ctx, tx, owner, r, key)
	return j, replay, err
}

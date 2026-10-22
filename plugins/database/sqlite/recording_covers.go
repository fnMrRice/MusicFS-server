package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func validRecordingCoverID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) && strings.IndexFunc(id, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}

func (s *Store) SelectRecordingCover(ctx context.Context, owner string, request library.SelectRecordingCoverRequest, key string) (library.RecordingCover, error) {
	var empty library.RecordingCover
	if owner == "" || !validRecordingCoverID(request.RecordingID) || request.ExpectedRevision < 0 || request.ExpectedRevision >= math.MaxInt64 || len(key) < 8 || !validRecordingCoverID(key) || request.ObjectID != nil && !validRecordingCoverID(*request.ObjectID) {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid recording cover selection")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	raw, _ := json.Marshal(request)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	const route = "recording/cover"
	var previous, expires string
	var replay []byte
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&previous, &replay, &expires)
	if err == nil {
		if previous != digest {
			return empty, domain.Fail("CONFLICT", "Idempotency key was used with another cover selection")
		}
		until, parseErr := time.Parse(time.RFC3339Nano, expires)
		if parseErr != nil || !until.After(time.Now()) {
			return empty, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new operation")
		}
		err = json.Unmarshal(replay, &empty)
		return empty, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	states, err := readRecordingCoverStates(ctx, tx, owner, []string{request.RecordingID})
	if err != nil {
		return empty, err
	}
	state, ok := states[request.RecordingID]
	if !ok {
		return empty, domain.Fail("NOT_FOUND", "Recording not found")
	}
	if request.ExpectedRevision != state.revision {
		return empty, domain.Fail("CONFLICT", "Recording cover revision changed")
	}
	if request.ObjectID != nil {
		valid := false
		for _, candidate := range state.candidates {
			valid = valid || candidate.ObjectID == *request.ObjectID
		}
		if !valid {
			return empty, domain.Fail("RESOURCE_CHANGED", "Cover is not a current recording member selection")
		}
	}
	state.revision++
	state.manual = request.ObjectID
	_, err = tx.ExecContext(ctx, `INSERT INTO recording_cover_preferences(owner,recording_id,revision,object_id) VALUES(?,?,?,?)
 ON CONFLICT(owner,recording_id) DO UPDATE SET revision=excluded.revision,object_id=excluded.object_id`, owner, request.RecordingID, state.revision, request.ObjectID)
	if err != nil {
		return empty, err
	}
	result := state.view(request.RecordingID)
	raw, err = json.Marshal(result)
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency(owner,route,key,digest,result,expires_at) VALUES(?,?,?,?,?,?)", owner, route, key, digest, raw, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return empty, err
	}
	return result, tx.Commit()
}

var _ library.RecordingCoverRepository = (*Store)(nil)

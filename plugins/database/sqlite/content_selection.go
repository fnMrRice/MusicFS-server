package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func contentRevision(ctx context.Context, tx *sql.Tx, owner, source string) (int64, error) {
	var revision int64
	err := tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT revision FROM media_selection_revisions WHERE owner=? AND source_id=?),0)", owner, source).Scan(&revision)
	return revision, err
}

func readContentSelection(ctx context.Context, tx *sql.Tx, owner, source string) (mediastore.Selection, error) {
	result := mediastore.Selection{Bindings: []mediastore.Binding{}}
	var err error
	if result.Target, err = contentTarget(ctx, tx, owner, source); err != nil {
		return result, err
	}
	if result.Revision, err = contentRevision(ctx, tx, owner, source); err != nil {
		return result, err
	}
	var assetCurrent bool
	if err = tx.QueryRowContext(ctx, "SELECT current FROM source_assets WHERE owner=? AND id=?", owner, result.Target.AssetID).Scan(&assetCurrent); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.document,s.manual_locked,o.state,b.current FROM media_content_slots s JOIN media_content_bindings b ON b.id=s.binding_id AND b.owner=s.owner JOIN media_content_objects o ON o.id=b.object_id AND o.owner=b.owner WHERE s.owner=? AND s.source_id=? ORDER BY s.field,s.language`, owner, source)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var locked, selected bool
		var state string
		if err = rows.Scan(&raw, &locked, &state, &selected); err != nil {
			return result, err
		}
		var b mediastore.Binding
		if err = json.Unmarshal(raw, &b); err != nil {
			return result, err
		}
		b.ManualLocked = locked
		b.Current = assetCurrent && selected && b.Target == result.Target
		b.State = state
		if state == "available" && !b.Current {
			b.State = "stale"
		}
		result.Bindings = append(result.Bindings, b)
	}
	return result, rows.Err()
}

func (s *Store) ContentSelection(ctx context.Context, owner, source string) (mediastore.Selection, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return mediastore.Selection{}, err
	}
	defer tx.Rollback()
	return readContentSelection(ctx, tx, owner, source)
}

func (s *Store) SelectContent(ctx context.Context, owner string, request mediastore.SelectRequest, key string) (mediastore.Selection, error) {
	return s.SelectContentGuarded(ctx, owner, request, key, func(publish func() error) error { return publish() })
}

// Production selection retains a validated immutable file across the commit.
// The unguarded wrapper is for trusted storage callers and database tests only.
func (s *Store) SelectContentGuarded(ctx context.Context, owner string, request mediastore.SelectRequest, key string, guard mediastore.Guard) (mediastore.Selection, error) {
	var empty mediastore.Selection
	if guard == nil {
		return empty, domain.Fail("INVALID_ARGUMENT", "Media selection requires a file guard")
	}
	// Automatic is deliberately absent from public JSON, but is part of the
	// durable operation identity so a replay cannot change the manual policy.
	m, replayed, err := s.beginSelection(ctx, owner, request, key)
	if err != nil {
		return empty, err
	}
	defer m.tx.Rollback()
	if replayed {
		return m.result, nil
	}
	if err = contentCurrentTarget(ctx, m.tx, owner, request.Target); err != nil {
		return empty, err
	}
	object, err := readContentObject(ctx, m.tx, owner, request.ObjectID)
	if err != nil {
		return empty, err
	}
	if object.Metadata.Target != request.Target {
		return empty, domain.Fail("RESOURCE_CHANGED", "Content belongs to a different source version")
	}
	if request.Online != nil && (object.Metadata.Field != request.Online.Field || object.Metadata.Origin.Kind != "scraper" || object.Metadata.Origin.InstanceID != request.Online.PluginID) {
		return empty, domain.Fail("RESOURCE_CHANGED", "Content does not match the scraper selection")
	}
	if object.State != "available" {
		return empty, domain.Fail("RESOURCE_CHANGED", "Content is unavailable")
	}
	field, language := object.Metadata.Field, object.Metadata.Language
	old, locked := contentSelectedSlot(m.result, field, language)
	if request.Automatic && (locked || (old != nil && old.Current && old.State == "available")) {
		return empty, domain.Fail("CONFLICT", "Automatic content cannot replace an existing or manually locked selection")
	}
	previous := ""
	if old != nil {
		previous = old.ID
	}
	var result mediastore.Selection
	var publicationErr error
	called := false
	err = guard(func() error {
		if called {
			return domain.Fail("CONFLICT", "Media selection was already published")
		}
		called = true
		result, publicationErr = m.selectObject(ctx, request, object, previous)
		return publicationErr
	})
	if err != nil {
		return empty, err
	}
	if !called {
		return empty, domain.Fail("CONFLICT", "Media selection guard did not publish")
	}
	if publicationErr != nil {
		return empty, publicationErr
	}
	return result, nil
}

func (m *contentMutation) selectObject(ctx context.Context, request mediastore.SelectRequest, object mediastore.ContentObject, previous string) (mediastore.Selection, error) {
	var empty mediastore.Selection
	owner, field, language := m.owner, object.Metadata.Field, object.Metadata.Language
	binding := mediastore.Binding{ID: domain.NewID(), ObjectID: object.ID, Target: request.Target, Field: field, Language: language, ManualLocked: !request.Automatic, Current: true, State: "available", CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(binding)
	if err != nil {
		return empty, err
	}
	if _, err = m.tx.ExecContext(ctx, "UPDATE media_content_bindings SET current=0 WHERE owner=? AND id=?", owner, previous); err != nil {
		return empty, err
	}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO media_content_bindings(id,owner,source_id,object_id,asset_id,field,language,current,document,created_at) VALUES(?,?,?,?,?,?,?,1,?,?)", binding.ID, owner, request.Target.SourceTrackID, object.ID, request.Target.AssetID, field, language, raw, binding.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return empty, err
	}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO media_content_slots(owner,source_id,field,language,binding_id,manual_locked) VALUES(?,?,?,?,?,?) ON CONFLICT(owner,source_id,field,language) DO UPDATE SET binding_id=excluded.binding_id,manual_locked=excluded.manual_locked", owner, request.Target.SourceTrackID, field, language, binding.ID, binding.ManualLocked)
	if err != nil {
		return empty, err
	}
	return m.finish(ctx, "select", field, language, previous, binding.ID, binding.ManualLocked)
}

func (s *Store) UnlockContent(ctx context.Context, owner string, request mediastore.UnlockRequest, key string) (mediastore.Selection, error) {
	request.Language = contentLanguage(request.Field, request.Language)
	m, replayed, err := s.beginContentMutation(ctx, owner, request.SourceTrackID, "unlock", key, request.ExpectedRevision, request)
	if err != nil {
		return mediastore.Selection{}, err
	}
	defer m.tx.Rollback()
	if replayed {
		return m.result, nil
	}
	format := "text"
	if request.Field == "artwork" {
		format = "png"
	}
	metadata := mediastore.ContentMetadata{Target: m.result.Target, Field: request.Field, Language: request.Language, Format: format, Origin: mediastore.Origin{Kind: "upload"}}
	if err = metadata.Validate(); err != nil {
		return mediastore.Selection{}, err
	}
	old, _ := contentSelectedSlot(m.result, request.Field, request.Language)
	if old == nil {
		return mediastore.Selection{}, domain.Fail("NOT_FOUND", "Media selection slot not found")
	}
	if _, err = m.tx.ExecContext(ctx, "UPDATE media_content_slots SET manual_locked=0 WHERE owner=? AND source_id=? AND field=? AND language=?", owner, request.SourceTrackID, request.Field, request.Language); err != nil {
		return mediastore.Selection{}, err
	}
	return m.finish(ctx, "unlock", request.Field, request.Language, old.ID, old.ID, false)
}

func contentSelectedSlot(selection mediastore.Selection, field, language string) (*mediastore.Binding, bool) {
	for _, binding := range selection.Bindings {
		if binding.Field == field && binding.Language == language {
			return &binding, binding.ManualLocked
		}
	}
	return nil, false
}

func advanceContentRevision(ctx context.Context, tx *sql.Tx, owner, source string) (int64, error) {
	if _, err := tx.ExecContext(ctx, "INSERT INTO media_selection_revisions(owner,source_id,revision) VALUES(?,?,1) ON CONFLICT(owner,source_id) DO UPDATE SET revision=revision+1", owner, source); err != nil {
		return 0, err
	}
	return contentRevision(ctx, tx, owner, source)
}

func recordContentChange(ctx context.Context, tx *sql.Tx, owner, source string, revision int64, operation, field, language, old, next string, locked bool) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO media_selection_changes(owner,source_id,revision,operation,field,language,previous_binding_id,next_binding_id,manual_locked,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", owner, source, revision, operation, field, language, strptr(old), strptr(next), locked, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

var _ mediastore.ContentRepository = (*Store)(nil)

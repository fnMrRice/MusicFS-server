package storage_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func TestContentPreparedBytesVersionAndBodyIsolation(t *testing.T) {
	s, items, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	body := "UNIQUE_PRIVATE_歌词正文_987654321"
	input := contentInput(tracks[0], "zh-HANS", body)
	p, existing, err := s.PrepareContent(ctx, "admin", input, "upload-replay-01")
	if err != nil || existing || p.Target.Purpose != "content" || p.Object.Size != 0 || p.Object.SHA256 != "" {
		t.Fatal("invalid prepared intent", err)
	}
	variant := input
	variant.Metadata.Language = "ZH-hans"
	replay, existing, err := s.PrepareContent(ctx, "admin", variant, "upload-replay-01")
	if err != nil || !existing || !reflect.DeepEqual(p, replay) {
		t.Fatal("canonical upload retry changed publication", err)
	}
	changed := input
	changed.Metadata.Origin.Kind = "edit"
	_, _, err = s.PrepareContent(ctx, "admin", changed, "upload-replay-01")
	matchingError(t, err, "CONFLICT")
	wrong := p.Object
	wrong.Size, wrong.SHA256 = input.Size, strings.Repeat("a", 64)
	err = s.WrittenPublication(ctx, "admin", p.ID, wrong)
	matchingError(t, err, "RESOURCE_CHANGED")
	stored, err := s.GetPublication(ctx, "admin", p.ID)
	if err != nil || stored.Status != "prepared" {
		t.Fatal("bad bytes advanced publication", err)
	}
	wrong.SHA256 = input.SHA256
	if err = s.WrittenPublication(ctx, "admin", p.ID, wrong); err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationStage(ctx, "admin", p.ID, "written", "published"); err != nil {
		t.Fatal(err)
	}
	items[0].ObservedVersion = "v2"
	publishEvidenceTracks(t, s, items...)
	err = s.ReferencePublication(ctx, "admin", p.ID)
	matchingError(t, err, "RESOURCE_CHANGED")
	_, err = s.ContentObject(ctx, "admin", p.Object.ID)
	matchingError(t, err, "NOT_FOUND")
	// No body or Base64 body may appear in any content/journal/idempotency record.
	for _, query := range []string{"SELECT document FROM media_publications", "SELECT document FROM media_content_objects", "SELECT document FROM media_content_bindings", "SELECT result FROM idempotency"} {
		rows, err := s.DB.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), body) || strings.Contains(string(raw), base64.StdEncoding.EncodeToString([]byte(body))) {
				t.Fatal("content body entered database")
			}
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	var metadata mediastore.UploadInput
	if err = json.Unmarshal(p.Target.Metadata, &metadata); err != nil || metadata.Metadata.Language != "zh-hans" || metadata.SHA256 != input.SHA256 {
		t.Fatal("journal binding differs", err)
	}
}

func TestContentRevisionCompetitionAndOperationCancellation(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	one := contentPublish(t, s, "admin", contentInput(tracks[0], "en", "first"))
	two := contentPublish(t, s, "admin", contentInput(tracks[0], "en", "second"))
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, o := range []mediastore.ContentObject{one, two} {
		wg.Add(1)
		go func(i int, o mediastore.ContentObject) {
			defer wg.Done()
			_, errs[i] = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: o.Metadata.Target, ObjectID: o.ID}, domain.NewID())
		}(i, o)
	}
	wg.Wait()
	winner, loser := 0, 1
	if errs[0] != nil {
		winner, loser = 1, 0
	}
	if errs[winner] != nil {
		t.Fatal("no selection committed", errs)
	}
	matchingError(t, errs[loser], "CONFLICT")
	selection, _ := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if selection.Revision != 1 || len(selection.Bindings) != 1 {
		t.Fatal("concurrent selection partially committed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err := s.PrepareContent(canceled, "admin", contentInput(tracks[0], "ja", "canceled"), domain.NewID())
	if err == nil {
		t.Fatal("canceled preparation persisted")
	}
	_, err = s.UnlockContent(canceled, "admin", mediastore.UnlockRequest{SourceTrackID: tracks[0].ID, ExpectedRevision: 1, Field: "lyrics", Language: "en"}, domain.NewID())
	if err == nil {
		t.Fatal("canceled unlock persisted")
	}
	var changes, bindings int
	s.DB.QueryRow("SELECT COUNT(*) FROM media_selection_changes").Scan(&changes)
	s.DB.QueryRow("SELECT COUNT(*) FROM media_content_bindings").Scan(&bindings)
	if changes != 1 || bindings != 1 {
		t.Fatal("failed operation leaked audit or binding")
	}
}

package storage_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

func TestWritebackQueueBindsScopeContentAndPrivateTarget(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	key := "writeback-operation-key"
	j, replay, err := f.s.QueueWriteback(ctx, "admin", f.request, f.target, key)
	if err != nil || replay || len(j.ID) != 48 || j.Status != "queued" {
		t.Fatal(j, err)
	}
	public, _ := json.Marshal(j)
	for _, secret := range []string{"relativePath", "fileIdentity", "externalId", "Selected English lyrics", f.target.FileIdentity} {
		if strings.Contains(string(public), secret) {
			t.Fatal("private writeback state exposed", secret)
		}
	}
	again, replay, err := f.s.QueueWriteback(ctx, "admin", f.request, f.target, key)
	if err != nil || !replay || again.ID != j.ID {
		t.Fatal("queue replay failed", err)
	}
	changed := f.request
	changed.Overwrite = true
	_, _, err = f.s.ReplayWriteback(ctx, "admin", changed, key)
	matchingError(t, err, "CONFLICT")
	other := f.target
	other.RelativePath = "different.mp3"
	_, _, err = f.s.QueueWriteback(ctx, "admin", f.request, other, key)
	matchingError(t, err, "CONFLICT")
	if _, err = f.s.GetWriteback(ctx, "bob", j.ID); err == nil {
		t.Fatal("private job exposed")
	}
	if _, _, err = f.s.QueueWriteback(ctx, "bob", f.request, f.target, domain.NewID()); err == nil {
		t.Fatal("foreign source queued")
	}
	work, err := f.s.NextWriteback(ctx)
	if err != nil || work.Job.ID != j.ID || work.Job.Status != "queued" || work.ExternalID != f.track.ExternalID || len(work.Objects) != 1 || work.Objects[0].ObjectID != f.english.ID {
		t.Fatal(work, err)
	}
	if _, err = f.s.CancelWriteback(ctx, "admin", j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.CancelWriteback(ctx, "admin", j.ID); err != nil {
		t.Fatal("cancel replay failed", err)
	}
	if err = f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 2); err == nil {
		t.Fatal("cancelled job began")
	}
	duplicate := f.request
	duplicate.Fields = []writebackhost.Field{{Name: "lyrics", Language: "en"}, {Name: "lyrics", Language: "und"}}
	if _, _, err = f.s.QueueWriteback(ctx, "admin", duplicate, f.target, domain.NewID()); err == nil {
		t.Fatal("multiple lyrics language silently aggregated")
	}
	missing := f.request
	missing.Fields = []writebackhost.Field{{Name: "artwork"}}
	if _, _, err = f.s.QueueWriteback(ctx, "admin", missing, f.target, domain.NewID()); err == nil {
		t.Fatal("unselected artwork queued")
	}
}

func TestWritebackFenceAllowsQueueAndReplayButBlocksNewMutations(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	j := f.queue(t)
	// Queued jobs do not freeze content; a changed revision invalidates Begin.
	if _, err := f.s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: f.track.ID, ExpectedRevision: 2, Field: "lyrics", Language: "en"}, domain.NewID()); err != nil {
		t.Fatal("queued job blocked mutation", err)
	}
	if err := f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 2); err == nil {
		t.Fatal("stale queued selection began")
	}
	f.request.ExpectedRevision = 3
	j = f.queue(t)
	if err := f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CancelWriteback(ctx, "admin", j.ID); err == nil {
		t.Fatal("running job cancelled")
	}
	_, err := f.s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: f.track.ID, ExpectedRevision: 3, Field: "lyrics", Language: "en"}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	_, err = f.s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: f.english.Metadata.Target, ObjectID: f.english.ID, ExpectedRevision: 3}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	replay, err := f.s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: f.english.Metadata.Target, ObjectID: f.english.ID, ExpectedRevision: 0}, "before-writeback-selection")
	if err != nil || replay.Revision != 1 {
		t.Fatal("active writeback blocked successful replay", err)
	}
	if err = f.s.SetWritebackState(ctx, "admin", j.ID, "recovery", "TEMPORARY_FAILURE"); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: f.track.ID, ExpectedRevision: 3, Field: "lyrics", Language: "en"}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	if err = f.s.SetWritebackState(ctx, "admin", j.ID, "failed", "RESOURCE_CHANGED"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: f.track.ID, ExpectedRevision: 3, Field: "lyrics", Language: "en"}, domain.NewID()); err != nil {
		t.Fatal("failed writeback retained mutation fence", err)
	}
}

func TestWritebackActiveQueueLimitIsBounded(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	for i := 0; i < 128; i++ {
		f.queue(t)
	}
	_, _, err := f.s.QueueWriteback(ctx, "admin", f.request, f.target, domain.NewID())
	matchingError(t, err, "RATE_LIMITED")
}

func TestWritebackDeferralLetsOtherQueuedJobsProceed(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	first, second := f.queue(t), f.queue(t)
	if err := f.s.DeferWriteback(ctx, "admin", first.ID); err != nil {
		t.Fatal(err)
	}
	next, err := f.s.NextWriteback(ctx)
	if err != nil || next.Job.ID != second.ID {
		t.Fatal("deferred head blocked another job", err)
	}
	if err = f.s.BeginWriteback(ctx, "admin", second.ID, f.target, 2); err != nil {
		t.Fatal(err)
	}
	if err = f.s.DeferWriteback(ctx, "admin", second.ID); err == nil {
		t.Fatal("running job was deferred")
	}
}

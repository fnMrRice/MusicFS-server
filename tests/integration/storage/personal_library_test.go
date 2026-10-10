package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/personal"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestPersonalCollectionsPersistenceIsolationCursorAndRetention(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	for _, track := range tracks {
		if _, err := s.PutFavorite(ctx, "admin", track.ID); err != nil {
			t.Fatal(err)
		}
	}
	p, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 1)
	if err != nil || p.NextCursor == nil {
		t.Fatal("missing first cursor", err)
	}
	if _, err = s.PersonalCollection(ctx, "other", personal.Favorites, *p.NextCursor, 1); err == nil {
		t.Fatal("cross-user cursor accepted")
	}
	if _, err = s.PutFavorite(ctx, "other", tracks[0].ID); err == nil {
		t.Fatal("cross-user favorite accepted")
	}
	if err = s.DeletePersonalCollection(ctx, "other", personal.Favorites, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PersonalCollection(ctx, "admin", personal.Favorites, *p.NextCursor, 1); err != nil {
		t.Fatal("other owner mutation invalidated cursor", err)
	}
	if _, err = s.DB.Exec("UPDATE library_revision SET revision=revision+1"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PersonalCollection(ctx, "admin", personal.Favorites, *p.NextCursor, 1); err == nil {
		t.Fatal("changed library cursor accepted")
	}
	p, err = s.PersonalCollection(ctx, "admin", personal.Favorites, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE personal_collection_cursors SET expires_at='2000-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PersonalCollection(ctx, "admin", personal.Favorites, *p.NextCursor, 1); err == nil {
		t.Fatal("expired cursor accepted")
	}
	now := time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < personal.MaxRecent+1; i++ {
		if _, err = tx.Exec("INSERT INTO personal_recently_played VALUES(?,?,?)", "admin", fmt.Sprintf("synthetic-%04d", i), now.Add(-time.Duration(i)*time.Second).Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec("INSERT INTO personal_recently_played VALUES('admin','expired',?)", now.Add(-91*24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("INSERT INTO personal_recently_played VALUES('other','private',?)", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PersonalCollection(ctx, "admin", personal.Recent, "", 100); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM personal_recently_played WHERE owner='admin'").Scan(&count); err != nil || count != personal.MaxRecent {
		t.Fatal("history retention count", count, err)
	}
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM personal_recently_played WHERE owner='admin' AND source_track_id IN ('expired','synthetic-1000')").Scan(&count); err != nil || count != 0 {
		t.Fatal("history retention removed wrong entries", count, err)
	}
	if err = s.DeletePersonalCollection(ctx, "admin", personal.Recent, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM personal_recently_played WHERE owner='other'").Scan(&count); err != nil || count != 1 {
		t.Fatal("history clear touched another user", err)
	}
	nickname := "Alice"
	if _, err = s.SavePersonalProfile(ctx, "admin", &nickname); err != nil {
		t.Fatal(err)
	}
	profile, err := s.PersonalProfile(ctx, "admin")
	if err != nil || profile.Nickname == nil || *profile.Nickname != nickname {
		t.Fatal("profile read after write", err)
	}
}

func TestPersonalHistoryValidatesSessionAndDeduplicatesAcrossAttempts(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	who := auth.Session{ID: "login", User: auth.User{ID: "admin"}}
	d := playback.Description{SessionID: domain.NewID(), AttemptID: domain.NewID(), AssetID: tracks[0].AssetID}
	save := func(state string, expiry time.Time) {
		t.Helper()
		raw, _ := json.Marshal(d)
		if err := s.PlaybackSave(ctx, d.SessionID, who.ID, who.User.ID, raw, state, expiry); err != nil {
			t.Fatal(err)
		}
	}
	save("active", time.Now().Add(time.Hour))
	input := personal.PlaybackStarted{SourceTrackID: tracks[0].ID, PlaybackSessionID: d.SessionID, AttemptID: d.AttemptID}
	first, err := s.RecordPlaybackStarted(ctx, who, input)
	if err != nil {
		t.Fatal(err)
	}
	d.AttemptID = domain.NewID()
	input.AttemptID = d.AttemptID
	save("active", time.Now().Add(time.Hour))
	again, err := s.RecordPlaybackStarted(ctx, who, input)
	if err != nil || !again.RecordedAt.Equal(first.RecordedAt) {
		t.Fatal("seek attempt duplicated history", err)
	}
	wrong := who
	wrong.ID = "another-login"
	if _, err = s.RecordPlaybackStarted(ctx, wrong, input); err == nil {
		t.Fatal("another login reported playback")
	}
	wrong = who
	wrong.User.ID = "other"
	if _, err = s.RecordPlaybackStarted(ctx, wrong, input); err == nil {
		t.Fatal("another owner reported playback")
	}
	save("stopped", time.Now().Add(time.Hour))
	if _, err = s.RecordPlaybackStarted(ctx, who, input); err == nil {
		t.Fatal("stopped session accepted")
	}
	if _, err = s.DB.Exec("UPDATE playback_sessions SET state='active',expires_at='2000-01-01T00:00:00Z' WHERE id=?", d.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordPlaybackStarted(ctx, who, input); err == nil {
		t.Fatal("expired session accepted")
	}
}

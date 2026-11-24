package storage_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func TestPlaylistCatalogLinksMergeMembersOnceWithoutEcho(t *testing.T) {
	for _, favorites := range []bool{true, false} {
		name := "custom"
		if favorites {
			name = "favorites"
		}
		t.Run(name, func(t *testing.T) {
			store := evidenceStore(t)
			ctx := t.Context()
			b := remoteBinding()
			b.AccountID = "account"
			local, err := store.PublishOnlineTrack(ctx, "owner", "other", online.Track{ExternalID: "existing", Title: "Local member"})
			if err != nil {
				t.Fatal(err)
			}
			target := personal.Favorites
			if favorites {
				if _, err := store.PutFavorite(ctx, "owner", local.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				refs, err := store.EnsurePlaylistTrackSources(ctx, "owner", b.InstanceID, b.AccountID, []string{"b"})
				if err != nil {
					t.Fatal(err)
				}
				existing := localBinding(local.ID)
				existing.Entries = append(existing.Entries, playlists.LocalEntry{Source: playlists.SourceRef{RemoteSourceID: &refs[0].ID}})
				target = playlistCreate(t, store, "owner", "Local", existing).ID
			}
			var revision int64
			for i := range 2 {
				link, err := store.LinkPlaylist(ctx, "owner", b, target)
				if err != nil || link.JobID == "" {
					t.Fatalf("missing sync job: %+v %v", link, err)
				}
				job, err := store.ClaimRemoteSync(ctx)
				if err != nil || job.ID != link.JobID {
					t.Fatalf("wrong job: %+v %v", job, err)
				}
				o := remoteObservation(1000+int64(i), "b", "b", "a")
				missing := "NOT_FOUND"
				o.Tracks[1].Track = nil
				o.Tracks[1].ErrorCode = &missing
				if _, err = store.FinishRemoteSync(ctx, job, o); err != nil {
					t.Fatal(err)
				}
				p, err := store.Playlist(ctx, "owner", target)
				if err != nil || p.Stale {
					t.Fatalf("playlist not published: %+v %v", p, err)
				}
				snap, err := store.PlaylistSnapshot(ctx, "owner", target, p.SnapshotRevision)
				if err != nil || len(snap.Entries) != 3 {
					t.Fatalf("unexpected members: %+v %v", snap, err)
				}
				foundMetadata, foundMissing := false, false
				for _, entry := range snap.Entries {
					if entry.Source.TrackID == nil {
						continue
					}
					track, err := store.MusicTrack(ctx, "owner", *entry.Source.TrackID)
					if err != nil || len(track.Sources) != 1 {
						t.Fatalf("missing source: %+v %v", track, err)
					}
					if track.Title == "Track b" && track.SelectedSource != nil {
						foundMetadata = true
					}
					if track.Title == "a" && track.Sources[0].SourceID == "a" {
						foundMissing = true
					}
				}
				if !foundMetadata || !foundMissing {
					t.Fatalf("metadata or unavailable member missing: %+v", snap)
				}
				if i > 0 && p.SnapshotRevision != revision {
					t.Fatal("second sync changed identical playlist")
				}
				revision = p.SnapshotRevision
				if addition, err := store.NextPlaylistAddition(ctx); err != nil || addition != nil {
					t.Fatalf("import echoed back to platform: %+v %v", addition, err)
				}
			}
			if !favorites {
				snap, err := store.PlaylistSnapshot(ctx, "owner", target, revision)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range snap.Entries {
					if entry.Provenance[0].Source.RemoteSourceID != nil {
						hidden, err := store.HidePlaylistSource(ctx, "owner", target, playlists.HideRequest{Source: entry.Source, Scope: "allSources"}, "hide-imported-track")
						if err != nil || len(hidden.Items) != 1 || hidden.Items[0].Source.RemoteSourceID == nil {
							t.Fatalf("playback selection lost remote hide target: %+v %v", hidden, err)
						}
					}
				}
			}
		})
	}
}

func TestPlaylistCatalogRecoversRateLimitedFavoritesAfterCooldown(t *testing.T) {
	for name, delay := range map[string]int{"historical": 0, "provider_cooldown": 600} {
		t.Run(name, func(t *testing.T) {
			s, ctx, b := evidenceStore(t), t.Context(), remoteBinding()
			b.AccountID = "account"
			link, err := s.LinkPlaylist(ctx, "owner", b, personal.Favorites)
			if err != nil {
				t.Fatal(err)
			}
			failed, err := s.ClaimRemoteSync(ctx)
			if err != nil || failed.ID != link.JobID {
				t.Fatal("missing initial sync", err)
			}
			failure := &domain.Error{Code: "RATE_LIMITED", Message: "Too many requests"}
			if delay > 0 {
				failure.RetryAfter = &delay
			}
			if err := s.FailRemoteSync(ctx, failed, failure); err != nil {
				t.Fatal(err)
			}
			prepare := func(account string) {
				t.Helper()
				if err := s.PreparePlaylistCatalog(ctx, "owner", b.InstanceID, b.Platform, b.ConfigRevision, account); err != nil {
					t.Fatal(err)
				}
			}
			age := func(duration time.Duration) {
				t.Helper()
				_, err := s.DB.ExecContext(ctx, `UPDATE remote_sync_jobs SET document=json_set(document,'$.Job.updatedAt',?) WHERE id=?`, time.Now().UTC().Add(-duration).Format(time.RFC3339Nano), failed.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			noJob := func() {
				t.Helper()
				if _, err := s.ClaimRemoteSync(ctx); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("unexpected recovery job: %v", err)
				}
			}
			if delay > 0 {
				age(2 * time.Minute)
			}
			prepare("")
			noJob()
			age(time.Hour)
			prepare("other-account")
			noJob()
			prepare("")
			prepare(b.AccountID)
			next, err := s.ClaimRemoteSync(ctx)
			if err != nil || next.ID == failed.ID || next.PlaylistID != failed.PlaylistID {
				t.Fatal("missing retry for the existing link", err)
			}
			if _, err := s.FinishRemoteSync(ctx, next, remoteObservation(1000, "first", "second")); err != nil {
				t.Fatal(err)
			}
			p, err := s.Playlist(ctx, "owner", personal.Favorites)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := s.PlaylistSnapshot(ctx, "owner", personal.Favorites, p.SnapshotRevision)
			if err != nil || len(snapshot.Entries) != 2 {
				t.Fatalf("favorites not restored: %+v %v", snapshot, err)
			}
			prepare("")
			noJob()
		})
	}
}

func TestPlaylistCatalogRecoveryAndAtomicFailure(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	b := remoteBinding()
	b.AccountID = "account"
	// 两种历史记录都没有曲库同步任务：只保存的绑定，以及曾经发布远端快照的歌单。
	p := remoteImport(t, s, "owner", b)
	if _, err := s.CancelRemoteSync(ctx, "owner", p.JobID); err != nil {
		t.Fatal(err)
	}
	sources, err := s.EnsurePlaylistTrackSources(ctx, "owner", b.InstanceID, b.AccountID, []string{"first"})
	if err != nil {
		t.Fatal(err)
	}
	prior, err := playlists.Reconcile(playlists.Snapshot{}, remoteObservation(900, "first"), playlists.Snapshot{PlaylistID: p.ID, Revision: 1, BindingRevision: 1, ObservationID: "previous"}, map[string]playlists.RemoteSource{"first": sources[0]})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, "INSERT INTO remote_playlist_snapshots VALUES(?,1,?,?)", p.ID, "owner", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE remote_playlists SET revision=1,document=json_set(document,'$.Playlist.revision',1),catalog_job_id=NULL WHERE id=?", p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO playlist_links(owner,platform,instance_id,account_id,external_id) VALUES(?,?,?,?,?)`, "owner", b.Platform, b.InstanceID, b.AccountID, b.ExternalID); err != nil {
		t.Fatal(err)
	}
	prepare := func(account string) {
		t.Helper()
		if err := s.PreparePlaylistCatalog(ctx, "owner", b.InstanceID, b.Platform, b.ConfigRevision, account); err != nil {
			t.Fatal(err)
		}
	}
	prepare("")
	job, err := s.ClaimRemoteSync(ctx)
	if err != nil || job.PlaylistID != p.ID {
		t.Fatalf("missing recovery job: %+v %v", job, err)
	}
	if err = s.FailRemoteSync(ctx, job, &domain.Error{Code: "PLATFORM_AUTH_REQUIRED", Message: "Login required"}); err != nil {
		t.Fatal(err)
	}
	prepare("")
	prepare("different")
	if _, err := s.ClaimRemoteSync(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("wrong platform account resumed sync: %v", err)
	}
	prepare("account")
	job, err = s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := remoteObservation(1000, "first", "second")
	o.Tracks[1].ExternalID = "unexpected"
	if _, err = s.FinishRemoteSync(ctx, job, o); err == nil {
		t.Fatal("incomplete metadata committed")
	}
	tracks, err := s.ListTracks(ctx, "owner", "", "", 100)
	if err != nil || len(tracks.Items) != 0 {
		t.Fatal("partial catalog publication", err)
	}
	if _, err = s.FinishRemoteSync(ctx, job, remoteObservation(1001, "first", "second")); err != nil {
		t.Fatal(err)
	}
	prepare("account")
	if _, err := s.ClaimRemoteSync(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("completed import queued again: %v", err)
	}
}

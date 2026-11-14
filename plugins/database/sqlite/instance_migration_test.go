package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func migrationStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func migrationSQL(t *testing.T, store *sqlite.Store, query string, args ...any) {
	t.Helper()
	if _, err := store.DB.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func migrationJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func legacyTrack(t *testing.T, store *sqlite.Store, id, provider string) {
	t.Helper()
	track := library.Track{ID: id, ProviderID: provider, DisplayName: "Unchanged title", AssetID: "asset-" + id, Availability: "available"}
	migrationSQL(t, store, "INSERT INTO tracks VALUES(?,?,?,?,?,?,?,?,?,?,?)", id, "admin", provider, id, "music", id+".flac", "v1", track.AssetID, "audio/flac", 12, migrationJSON(t, track))
}

func TestInstanceMigrationKeepsLibraryAndRemoteIdentities(t *testing.T) {
	ctx := context.Background()
	store := migrationStore(t)
	legacyTrack(t, store, "track-one", "local")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	migrationSQL(t, store, "INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,position,created_at) VALUES('admin','favorites','','track-one','track-one',0,?)", now)
	migrationSQL(t, store, "INSERT INTO source_albums VALUES('album-one','admin','local','Album',1,?,?)", now, now)
	migrationSQL(t, store, "INSERT INTO source_album_members VALUES('album-one','track-one',0)")
	plan := testkit.ScanPlan("music")
	plan.PluginID = "musicfs.localfiles"
	job, _, err := store.Enqueue(ctx, "admin", "local", plan, "legacy-scan-request")
	if err != nil {
		t.Fatal(err)
	}
	var savedPlan []byte
	var savedDigest string
	if err := store.DB.QueryRow("SELECT plan,plan_digest FROM jobs WHERE id=?", job.ID).Scan(&savedPlan, &savedDigest); err != nil {
		t.Fatal(err)
	}
	binding := playlists.Binding{RemoteRef: playlists.RemoteRef{InstanceID: "net", AccountID: "account", ExternalID: "123"}, Platform: "netease", ConfigRevision: "legacy-binding"}
	playlist, _, err := store.ImportRemotePlaylist(ctx, "admin", binding, "legacy-remote-import")
	if err != nil {
		t.Fatal(err)
	}
	syncJob, _, err := store.QueueRemoteSync(ctx, "admin", playlist.ID, playlist.BindingRevision, binding, "legacy-sync-request")
	if err != nil {
		t.Fatal(err)
	}
	source := playlists.RemoteSource{ID: "remote-one", RemoteRef: playlists.RemoteRef{InstanceID: "net", AccountID: "account", ExternalID: "456"}}
	migrationSQL(t, store, "INSERT INTO remote_sources VALUES(?,?,?,?,?,?)", source.ID, "admin", source.InstanceID, source.AccountID, source.ExternalID, migrationJSON(t, source))
	snapshot := playlists.Snapshot{PlaylistID: playlist.ID, Revision: 1, BindingRevision: 1, MetadataDigest: "preserved-metadata-digest", Entries: []playlists.Entry{{ID: "entry-one", Source: source}}}
	migrationSQL(t, store, "INSERT INTO remote_playlist_snapshots VALUES(?,?,?,?)", playlist.ID, 1, "admin", migrationJSON(t, snapshot))
	migrationSQL(t, store, "INSERT INTO library_snapshots VALUES('snapshot','admin','local','1',?)", now)
	migrationSQL(t, store, "INSERT INTO library_snapshot_items VALUES('snapshot',0,?)", migrationJSON(t, library.Track{ID: "track-one", ProviderID: "local"}))
	if err := store.MigratePluginInstanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	track, err := store.FindTrack(ctx, "admin", "track-one")
	if err != nil || track.ProviderID != instanceid.Canonical("local") || track.AssetID != "asset-track-one" || track.DisplayName != "Unchanged title" {
		t.Fatalf("track identity or content changed: %+v %v", track, err)
	}
	album, err := store.SourceAlbum(ctx, "admin", "album-one")
	if err != nil || album.ProviderInstanceID != track.ProviderID || len(album.SourceTrackIDs) != 1 || album.SourceTrackIDs[0] != track.ID {
		t.Fatalf("album lost its source: %+v %v", album, err)
	}
	gotPlaylist, err := store.RemotePlaylist(ctx, "admin", playlist.ID)
	if err != nil || gotPlaylist.InstanceID != instanceid.Canonical("net") || gotPlaylist.ConfigRevision != binding.ConfigRevision || gotPlaylist.AccountID != "account" {
		t.Fatalf("remote playlist lost identity or binding history: %+v %v", gotPlaylist, err)
	}
	gotSource, err := store.RemoteSource(ctx, "admin", source.ID)
	if err != nil || gotSource.InstanceID != gotPlaylist.InstanceID || gotSource.ID != source.ID {
		t.Fatalf("remote source lost identity: %+v %v", gotSource, err)
	}
	gotSnapshot, err := store.RemoteSnapshot(ctx, "admin", playlist.ID, 1)
	if err != nil || len(gotSnapshot.Entries) != 1 || gotSnapshot.Entries[0].ID != "entry-one" || gotSnapshot.Entries[0].Source != gotSource || gotSnapshot.MetadataDigest != snapshot.MetadataDigest {
		t.Fatalf("remote snapshot lost identity or digest: %+v %v", gotSnapshot, err)
	}
	failedScan, err := store.GetJob(ctx, "admin", job.ID)
	if err != nil || failedScan.Status != "failed" || failedScan.Error == nil || failedScan.Error.Code != "RESOURCE_CHANGED" || failedScan.ProviderInstanceID != track.ProviderID {
		t.Fatalf("old scan was not explicitly invalidated: %+v %v", failedScan, err)
	}
	var afterPlan []byte
	var afterDigest string
	if err := store.DB.QueryRow("SELECT plan,plan_digest FROM jobs WHERE id=?", job.ID).Scan(&afterPlan, &afterDigest); err != nil || string(savedPlan) != string(afterPlan) || savedDigest != afterDigest {
		t.Fatalf("captured scan plan or digest changed: %v", err)
	}
	failedSync, err := store.RemoteSyncJob(ctx, "admin", syncJob.ID)
	if err != nil || failedSync.State != "failed" || failedSync.Error == nil || failedSync.Error.Code != "RESOURCE_CHANGED" || failedSync.Binding != binding {
		t.Fatalf("old remote sync ticket was reinterpreted: %+v %v", failedSync, err)
	}
	var favorites, snapshots, revision int
	if err := store.DB.QueryRow("SELECT COUNT(*) FROM music_playlist_entries WHERE playlist_id='favorites' AND track_id='track-one'").Scan(&favorites); err != nil || favorites != 1 {
		t.Fatalf("favorite lost stable source identity: %v", err)
	}
	if err := store.DB.QueryRow("SELECT COUNT(*) FROM library_snapshot_items").Scan(&snapshots); err != nil || snapshots != 0 {
		t.Fatalf("stale query items were retained: %v", err)
	}
	if err := store.DB.QueryRow("SELECT revision FROM library_revision").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err := store.MigratePluginInstanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	var repeatedRevision int
	if err := store.DB.QueryRow("SELECT revision FROM library_revision").Scan(&repeatedRevision); err != nil || repeatedRevision != revision {
		t.Fatalf("repeated migration changed current records: %v", err)
	}
}

func TestInstanceMigrationRefusesNamespaceMergeAndPendingFileRecovery(t *testing.T) {
	for _, kind := range []string{"namespace", "file-recovery"} {
		t.Run(kind, func(t *testing.T) {
			store := migrationStore(t)
			legacyTrack(t, store, "track-one", "local")
			if kind == "namespace" {
				legacyTrack(t, store, "track-two", instanceid.Canonical("local"))
			} else {
				work := writebackhost.Work{Target: writeback.Target{SourceTrackID: "track-one", InstanceID: "local"}}
				migrationSQL(t, store, "INSERT INTO writeback_jobs(id,owner,source_id,state,old_size,work,created_at,updated_at) VALUES('write-one','admin','track-one','recovery',12,?,?,?)", migrationJSON(t, work), "2026-10-20T00:00:00Z", "2026-10-20T00:00:00Z")
			}
			err := store.MigratePluginInstanceIDs(context.Background())
			var detail *domain.Error
			if !errors.As(err, &detail) || detail.Code != "CONFLICT" {
				t.Fatalf("expected actionable conflict: %v", err)
			}
			track, err := store.FindTrack(context.Background(), "admin", "track-one")
			if err != nil || track.ProviderID != "local" {
				t.Fatalf("failed migration modified source data: %+v %v", track, err)
			}
			var provider string
			if err := store.DB.QueryRow("SELECT provider FROM tracks WHERE id='track-one'").Scan(&provider); err != nil || provider != "local" {
				t.Fatalf("failed migration modified indexed scope: %s %v", provider, err)
			}
		})
	}
}

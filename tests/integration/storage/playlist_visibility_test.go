package storage_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPlaylistVisibilityChecksEverySourceKindAndOwner(t *testing.T) {
	ctx := t.Context()
	store, _, tracks := matchingFixture(t, 1)
	remote := remoteImport(t, store, "admin", remoteBinding())
	remoteID := remotePublish(t, store, remote, remoteObservation(1000, "song")).Entries[0].Source.ID
	var musicID string
	if err := store.DB.QueryRowContext(ctx, "SELECT track_id FROM music_sources WHERE id=?", tracks[0].ID).Scan(&musicID); err != nil {
		t.Fatal(err)
	}
	binding := localBinding(tracks[0].ID, tracks[0].ID)
	binding.Entries = append(binding.Entries,
		playlists.LocalEntry{Source: playlists.SourceRef{TrackID: &musicID, ForcedSourceID: &tracks[0].ID}},
		playlists.LocalEntry{Source: playlists.SourceRef{RemoteSourceID: &remoteID}})
	p := playlistCreate(t, store, "admin", "Mixed", binding)
	saved := playlistBuild(t, store, "admin", p)
	got, err := store.PlaylistSnapshot(ctx, "admin", p.ID, saved.Revision)
	if err != nil || !reflect.DeepEqual(got, saved) {
		t.Fatal("mixed playlist snapshot changed", err)
	}
	if _, err = store.DB.ExecContext(ctx, "UPDATE remote_sources SET owner=? WHERE id=?", "other", remoteID); err != nil {
		t.Fatal(err)
	}
	_, err = store.PlaylistSnapshot(ctx, "admin", p.ID, saved.Revision)
	matchingError(t, err, "NOT_FOUND")
	_, err = store.Playlist(ctx, "admin", p.ID)
	matchingError(t, err, "NOT_FOUND")
}

func TestPostgresPlaylistSourceVisibility(t *testing.T) {
	ctx := t.Context()
	options := testkit.PostgresOptions(t)
	store, err := storage.Open(ctx, "postgres", storage.Options{DataRoot: t.TempDir(), Values: options})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := testkit.PostgresDatabase(t, options)
	if _, err = db.ExecContext(ctx, `INSERT INTO music_tracks(id,owner,title) VALUES
		('song-a','listener','First'),('song-b','listener','Second'),('private-song','other','Private')`); err != nil {
		t.Fatal(err)
	}
	first, second, private := "song-a", "song-b", "private-song"
	req := playlists.CreatePlaylistRequest{Name: "Mixed", Bindings: []playlists.PlaylistBinding{{Kind: "local", Entries: []playlists.LocalEntry{
		{Source: playlists.SourceRef{TrackID: &first}}, {Source: playlists.SourceRef{TrackID: &second}},
		{Source: playlists.SourceRef{TrackID: &first}},
	}}}}
	p, err := store.CreatePlaylist(ctx, "listener", req, "create-owned")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.RebuildPlaylist(ctx, "listener", p.ID, playlists.RebuildRequest{ExpectedRuleRevision: p.RuleRevision}, "rebuild-owned")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.PlaylistSnapshot(ctx, "listener", p.ID, saved.Revision)
	if err != nil || len(got.Entries) != 2 || len(got.Entries[0].Provenance) != 2 {
		t.Fatal("unexpected playlist snapshot", err, got.Entries)
	}
	req.Bindings[0].Entries[1].Source.TrackID = &private
	_, err = store.CreatePlaylist(ctx, "listener", req, "create-private")
	matchingError(t, err, "NOT_FOUND")
}

func BenchmarkPlaylistSnapshotVisibility(b *testing.B) {
	const count = 1000
	ctx := b.Context()
	store, err := sqlite.Open(ctx, filepath.Join(b.TempDir(), "playlist.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	_, err = store.DB.ExecContext(ctx, `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
		INSERT INTO music_tracks(id,owner,title) SELECT 'track-'||i,'listener','Track '||i FROM n`, count)
	if err != nil {
		b.Fatal(err)
	}
	var entries []playlists.LocalEntry
	for i := 1; i <= count; i++ {
		id := fmt.Sprintf("track-%d", i)
		entries = append(entries, playlists.LocalEntry{Source: playlists.SourceRef{TrackID: &id}})
	}
	p, err := store.CreatePlaylist(ctx, "listener", playlists.CreatePlaylistRequest{
		Name: "Library", Bindings: []playlists.PlaylistBinding{{Kind: "local", Entries: entries}},
	}, "playlist-create")
	if err != nil {
		b.Fatal(err)
	}
	snapshot, err := store.RebuildPlaylist(ctx, "listener", p.ID, playlists.RebuildRequest{ExpectedRuleRevision: p.RuleRevision}, "playlist-rebuild")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		page, err := store.PlaylistSnapshot(ctx, "listener", p.ID, snapshot.Revision)
		if err != nil || len(page.Entries) != count {
			b.Fatal("unexpected playlist snapshot", err, len(page.Entries))
		}
	}
}

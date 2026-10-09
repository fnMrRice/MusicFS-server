package storage_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestLibraryStableIdentitySnapshotAndPermissions(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items := []library.Observation{{Observation: pluginproto.Observation{ExternalID: "one", DisplayName: "same", RootID: "root", RelativePath: "a.wav", ObservedVersion: "v1"}}, {Observation: pluginproto.Observation{ExternalID: "two", DisplayName: "same", RootID: "root", RelativePath: "b.wav", ObservedVersion: "v1"}}}
	publish := func(key string) {
		_, _, err := s.Enqueue(ctx, "admin", "root", "scan", key)
		if err != nil {
			t.Fatal(err)
		}
		j, err := s.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Publish(ctx, j, items); err != nil {
			t.Fatal(err)
		}
		s.Finish(ctx, j, len(items), nil)
	}
	publish("scan-0001")
	first, err := s.ListTracks(ctx, "admin", "root", "", 1)
	if err != nil || first.NextCursor == nil {
		t.Fatal("missing page", err)
	}
	items[0].ObservedVersion = "v2"
	publish("scan-0002")
	second, err := s.ListTracks(ctx, "admin", "root", *first.NextCursor, 1)
	if err != nil || second.LibraryRevision != first.LibraryRevision || second.Items[0].ID == first.Items[0].ID {
		t.Fatal("snapshot drift", err)
	}
	if _, err = s.ListTracks(ctx, "other", "root", *first.NextCursor, 1); err == nil {
		t.Fatal("cross-user cursor accepted")
	}
	current, err := s.ListTracks(ctx, "admin", "root", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if current.LibraryRevision == first.LibraryRevision {
		t.Fatal("revision did not advance")
	}
	if _, err = s.FindTrack(ctx, "other", current.Items[0].ID); err == nil {
		t.Fatal("cross-user track accepted")
	}
}

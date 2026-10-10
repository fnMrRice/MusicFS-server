package storage_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/src/sourcedisplay"
)

func TestSourceDisplayOwnerIsolationOrderAndRemoteIdentity(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	private := matchingPrivateTrack(t, s)
	remote := remoteImport(t, s, "admin", remoteBinding())
	snapshot := remotePublish(t, s, remote, remoteObservation(1000, "42"))
	missing := domain.NewID()
	refs := []playlists.SourceRef{{SourceTrackID: &tracks[1].ID}, {SourceTrackID: &private.ID}, {SourceTrackID: &missing}, {RemoteSourceID: &snapshot.Entries[0].Source.ID}, {SourceTrackID: &tracks[1].ID}}
	page, err := sourcedisplay.New(s, nil).Lookup(context.Background(), "admin", refs)
	if err != nil || len(page.Items) != 5 {
		t.Fatal(page, err)
	}
	for _, i := range []int{0, 4} {
		if page.Items[i].Local == nil || page.Items[i].Local.ID != tracks[1].ID || page.Items[i].ErrorCode != nil {
			t.Fatal("local reference reordered", page)
		}
	}
	for _, i := range []int{1, 2} {
		if page.Items[i].Local != nil || page.Items[i].ErrorCode == nil || *page.Items[i].ErrorCode != "NOT_FOUND" {
			t.Fatal("foreign identity leaked", page)
		}
	}
	if page.Items[3].RemoteSource == nil || page.Items[3].RemoteTrack != nil || page.Items[3].ErrorCode == nil || *page.Items[3].ErrorCode != "SOURCE_OFFLINE" {
		t.Fatal("remote identity lost", page)
	}
	_, err = s.SourceDisplay(context.Background(), "admin", []playlists.SourceRef{{SourceTrackID: &tracks[0].ID, RemoteSourceID: &missing}})
	matchingError(t, err, "INVALID_ARGUMENT")
}

func TestPersonalPlaylistSearchFindsBeyondFirstPageAndIsOwnerScoped(t *testing.T) {
	s, _, _ := matchingFixture(t, 0)
	playlistCreate(t, s, "admin", "Unrelated")
	want := playlistCreate(t, s, "admin", "夜间 Älbum")
	playlistCreate(t, s, "other", "夜间 Älbum")
	for _, q := range []string{"夜间", "älBUM"} {
		items, err := s.SearchPlaylists(context.Background(), "admin", q, "", 1)
		if err != nil || len(items) != 1 || items[0].ID != want.ID {
			t.Fatal("full owner search failed", items, err)
		}
		more, err := s.SearchPlaylists(context.Background(), "admin", q, items[0].ID, 1)
		if err != nil || len(more) != 0 {
			t.Fatal("keyset repeated results", more, err)
		}
	}
}

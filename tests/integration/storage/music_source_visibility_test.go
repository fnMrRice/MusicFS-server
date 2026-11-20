package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

func TestMusicSourceOnlyFiltersBeforePaginationAndKeepsOfflineSources(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	available := visibilityObservation("available", "B available.wav", "Singer", "Scanned album", "Jazz", "wav")
	missing := visibilityObservation("missing", "D missing.wav", "Singer", "Scanned album", "Jazz", "wav")
	sources := visibilityScan(t, s, "admin", "local", available, missing)
	// 完整复扫中缺失的文件仍保留来源身份，与从未关联音频的资料条目区分。
	visibilityScan(t, s, "admin", "local", available)
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Shared album"}, "create-visibility-album")
	if err != nil {
		t.Fatal(err)
	}
	for i, title := range []string{"A metadata only", "C metadata only"} {
		key := []string{"create-first-metadata", "create-second-metadata"}[i]
		if _, err = s.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: title}, ArtistNames: []string{"Singer"}, AlbumIDs: []string{album.ID}}, key); err != nil {
			t.Fatal(err)
		}
	}
	for i, source := range sources {
		track, err := s.MusicTrackForSource(ctx, "admin", source.ID)
		if err != nil {
			t.Fatal(err)
		}
		key := []string{"assign-available-album", "assign-missing-album"}[i]
		if _, err = s.SaveMusicTrack(ctx, "admin", track.ID, track.Revision, music.TrackInput{TrackFields: track.TrackFields, ArtistNames: []string{"Singer"}, AlbumIDs: []string{album.ID}}, key); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.MusicTracks(ctx, "admin", music.Query{Limit: 20})
	if err != nil || len(all.Items) != 4 {
		t.Fatalf("all metadata query: %+v %v", all, err)
	}
	q := music.Query{Limit: 1, SourceOnly: true}
	first, err := s.MusicTracks(ctx, "admin", q)
	if err != nil || len(first.Items) != 1 || first.Items[0].SelectedSource == nil || first.Items[0].SelectedSource.ID != sources[0].ID || first.NextAfter == nil {
		t.Fatalf("first sourced page: %+v %v", first, err)
	}
	q.After = *first.NextAfter
	second, err := s.MusicTracks(ctx, "admin", q)
	if err != nil || len(second.Items) != 1 || second.Items[0].SelectedSource == nil || second.Items[0].SelectedSource.ID != sources[1].ID || second.NextAfter != nil {
		t.Fatalf("second sourced page: %+v %v", second, err)
	}
	if second.Items[0].SelectedSource.Availability == "available" || len(second.Items[0].Sources) != 1 {
		t.Fatalf("offline source identity was lost: %+v", second.Items[0])
	}
	q.SourceOnly = false
	if _, err = s.MusicTracks(ctx, "admin", q); err == nil {
		t.Fatal("cursor was reused with a different source filter")
	}
	artistID := all.Items[0].Artists[0].ID
	for _, category := range []music.Query{{Limit: 20, AlbumID: album.ID}, {Limit: 20, ArtistID: artistID}} {
		page, err := s.MusicTracks(ctx, "admin", category)
		if err != nil || len(page.Items) != 4 || page.Items[0].SelectedSource != nil {
			t.Fatalf("category lost source-less metadata: %+v %v", page, err)
		}
	}
	for _, scope := range []struct {
		owner string
		query music.Query
	}{{"other", music.Query{Limit: 20, SourceOnly: true}}, {"admin", music.Query{Limit: 20, ProviderID: "other", SourceOnly: true}}, {"admin", music.Query{Limit: 20, Q: "metadata only", SourceOnly: true}}} {
		page, err := s.MusicTracks(ctx, scope.owner, scope.query)
		if err != nil || len(page.Items) != 0 || page.NextAfter != nil {
			t.Fatalf("source filter crossed owner, provider or search scope: %+v %v", page, err)
		}
	}
}

package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestMusicAlbumSourceLinkSavesWithoutFetchingAndRejectsInvalidPairs(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	page, plugin := "https://example.com/release/one", "fixture"
	for _, in := range []music.AlbumFields{
		{Name: "Album", SourceLinks: []music.AlbumSourceLink{{URL: page, PluginID: plugin}, {URL: "https://example.com/two", PluginID: plugin}}},
		{Name: "Album", SourceLinks: []music.AlbumSourceLink{{URL: page}}}, {Name: "Album", SourceLinks: []music.AlbumSourceLink{{PluginID: plugin}}},
		{Name: "Album", SourceLinks: []music.AlbumSourceLink{{URL: "http://example.com/one", PluginID: plugin}}},
		{Name: "Album", SourceLinks: []music.AlbumSourceLink{{URL: "https://user:secret@example.com/one", PluginID: plugin}}},
		{Name: "Album", SourceLinks: []music.AlbumSourceLink{{URL: page, PluginID: "bad plugin"}}},
	} {
		if _, err := s.SaveMusicAlbum(ctx, "admin", "", 0, in, domain.NewID()); err == nil {
			t.Fatal("invalid source association was accepted")
		}
	}
	a, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Original", SourceLinks: []music.AlbumSourceLink{{URL: page, PluginID: plugin}, {URL: "https://example.org/album", PluginID: "second"}}}, "source-link-without-fetch")
	if err != nil || len(a.SourceLinks) != 2 || a.SourceLinks[0].URL != page || a.SourceLinks[0].PluginID != plugin {
		t.Fatalf("source link: %+v %v", a, err)
	}
	fields, err := s.MusicAlbumScrapedFields(ctx, "admin", a.ID)
	if err != nil || len(fields) != 0 {
		t.Fatalf("saving the link scraped metadata: %+v %v", fields, err)
	}
	in := a.AlbumFields
	in.Name = "Alias"
	in.SourceLinks[0].URL = "https://example.com/release/two"
	changed, err := s.SaveMusicAlbum(ctx, "admin", a.ID, a.Revision, in, "change-source-link")
	if err != nil || changed.OriginalName != "Original" || len(changed.SourceLinks) != 2 || changed.SourceLinks[0].URL != in.SourceLinks[0].URL || changed.SourceLinks[1].PluginID != "second" {
		t.Fatalf("source link update: %+v %v", changed, err)
	}
	in.SourceLinks = nil
	cleared, err := s.SaveMusicAlbum(ctx, "admin", a.ID, changed.Revision, in, "clear-source-link")
	if err != nil || len(cleared.SourceLinks) != 0 {
		t.Fatalf("clear association: %+v %v", cleared, err)
	}
}

func albumTestString(value string) *string { return &value }

func TestMusicAlbumMetadataMergePreservesUnselectedFieldsAndSupportsPartialDates(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	a, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Hand name", Publisher: albumTestString("Hand label"), ReleaseDate: albumTestString("2001-02-03")}, "create-selective-merge")
	if err != nil {
		t.Fatal(err)
	}
	old := albumBundleFields([]scraper.Track{{Title: "Old song", Number: 1}})
	old[0].Item.Artist, old[0].Item.Genre, old[0].Item.Description = "Old singer", "Old genre", "Old description"
	old[0].Item.Date, old[0].Item.Publisher, old[0].Item.SourceURL = "2001-02-03", "Old label", "https://example.com/old-release"
	if _, err = s.SelectMusicAlbumScrapedFields(ctx, "admin", a.ID, 1, old, "initial-merge-bundle"); err != nil {
		t.Fatal(err)
	}
	a, err = s.MusicAlbum(ctx, "admin", a.ID)
	if err != nil || len(a.SourceLinks) != 1 || a.SourceLinks[0].URL != old[0].Item.SourceURL || a.SourceLinks[0].PluginID != "fixture" {
		t.Fatalf("bundle source association: %+v %v", a, err)
	}
	manualURL := "https://example.com/manual-page"
	in := a.AlbumFields
	in.SourceLinks[0].URL = manualURL
	if _, err = s.SaveMusicAlbum(ctx, "admin", a.ID, 2, in, "manual-merge-link"); err != nil {
		t.Fatal(err)
	}
	oldArt := works.ScrapedField{Field: "artwork", PluginID: "fixture", CandidateToken: "old-art-token", Item: scraper.Item{ID: "release", Title: "Old artwork", ImageURL: "https://example.com/old.png"}}
	if _, err = s.SelectMusicAlbumScrapedField(ctx, "admin", a.ID, 3, oldArt, "old-merge-artwork"); err != nil {
		t.Fatal(err)
	}
	next := albumBundleFields([]scraper.Track{{Title: "New first", Number: 1}, {Title: "New second", Number: 2}})
	next[0].Item = scraper.Item{ID: "new-release", Title: "Online name", Artist: "New singer", Date: "2024-03-04T08:00:00Z", Publisher: "New label", Genre: "New genre", Description: "New description", ImageURL: "https://example.com/new.png", SourceURL: "https://example.com/new-release"}
	next[1].Item.ID = "new-release"
	next[0].CandidateToken, next[1].CandidateToken = "new-details-token", "new-tracks-token"
	merge := func(revision int64, fields []string, key string) {
		t.Helper()
		got, err := s.MergeMusicAlbumMetadata(ctx, "admin", a.ID, revision, next, fields, key)
		if err != nil || got != revision+1 {
			t.Fatalf("merge: %d %v", got, err)
		}
	}
	merge(4, []string{"publisher", "genre"}, "selective-fields-merge")
	a, err = s.MusicAlbum(ctx, "admin", a.ID)
	if err != nil || a.Name != "Hand name" || a.OriginalName != "Hand name" || a.Publisher == nil || *a.Publisher != "New label" || a.ReleaseDate == nil || *a.ReleaseDate != "2001-02-03" || len(a.SourceLinks) == 0 || a.SourceLinks[0].URL != manualURL || a.CoverPath == nil || *a.CoverPath != oldArt.Item.ImageURL {
		t.Fatalf("unselected album values changed: %+v %v", a, err)
	}
	readDetails := func() works.ScrapedField {
		t.Helper()
		saved, err := s.MusicAlbumScrapedFields(ctx, "admin", a.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range saved {
			if field.Field == "albumDetails" {
				return field
			}
		}
		t.Fatal("details missing")
		return works.ScrapedField{}
	}
	details := readDetails()
	if details.Item.Artist != "Old singer" || details.Item.Description != "Old description" || details.Item.Genre != "New genre" || details.Item.Date != "2001-02-03" || details.Item.ID != "new-release" {
		t.Fatalf("unselected saved values changed: %+v", details)
	}
	tracks, err := s.MusicTracks(ctx, "admin", music.Query{Limit: 20, AlbumID: a.ID})
	if err != nil || len(tracks.Items) != 1 || tracks.Items[0].Title != "Old song" {
		t.Fatalf("unselected tracks changed: %+v %v", tracks, err)
	}
	merge(5, []string{"cover"}, "select-cover-merge")
	a, err = s.MusicAlbum(ctx, "admin", a.ID)
	if err != nil || a.CoverPath == nil || *a.CoverPath != next[0].Item.ImageURL {
		t.Fatalf("old artwork shadowed the merged cover: %+v %v", a, err)
	}
	merge(6, []string{"name", "artist", "releaseDate", "description", "tracks"}, "select-main-and-tracks")
	a, err = s.MusicAlbum(ctx, "admin", a.ID)
	if err != nil || a.Name != "Online name" || a.OriginalName != "Hand name" || a.ReleaseDate == nil || *a.ReleaseDate != "2024-03-04" || len(a.SourceLinks) == 0 || a.SourceLinks[0].URL != manualURL {
		t.Fatalf("merged main values: %+v %v", a, err)
	}
	tracks, err = s.MusicTracks(ctx, "admin", music.Query{Limit: 20, AlbumID: a.ID})
	if err != nil || len(tracks.Items) != 3 || tracks.Items[0].Title != "New first" || tracks.Items[1].Title != "New second" || tracks.Items[2].Title != "Old song" {
		t.Fatalf("selected complete tracks not merged: %+v %v", tracks, err)
	}
	next[0].Item.Date = "1997-04"
	for i := range next {
		next[i].PluginID = "other-platform"
	}
	merge(7, []string{"releaseDate"}, "merge-partial-date")
	a, err = s.MusicAlbum(ctx, "admin", a.ID)
	if err != nil || a.ReleaseDate != nil || readDetails().Item.Date != "1997-04" {
		t.Fatalf("partial date precision lost: %+v %v", a, err)
	}
	if len(a.SourceLinks) != 2 || a.SourceLinks[0].URL != manualURL || a.SourceLinks[1].PluginID != "other-platform" || a.SourceLinks[1].URL != next[0].Item.SourceURL {
		t.Fatalf("merge did not retain both platforms: %+v", a.SourceLinks)
	}
	for i := range next {
		next[i].PluginID = "fixture"
	}
	next[0].Item.Date = "2000-99"
	if _, err = s.MergeMusicAlbumMetadata(ctx, "admin", a.ID, 8, next, []string{"releaseDate"}, "reject-invalid-date"); err == nil {
		t.Fatal("invalid partial date accepted")
	}
	for _, fields := range [][]string{nil, {"unknown"}, {"name", "name"}} {
		if _, err = s.MergeMusicAlbumMetadata(ctx, "admin", a.ID, 8, next, fields, domain.NewID()); err == nil {
			t.Fatal("invalid merge fields accepted")
		}
	}
	expired := []works.ScrapedField{next[0], next[1]}
	expired[0].Item, expired[1].Item = scraper.Item{}, scraper.Item{}
	if revision, err := s.MergeMusicAlbumMetadata(ctx, "admin", a.ID, 4, expired, []string{"genre", "publisher"}, "selective-fields-merge"); err != nil || revision != 5 {
		t.Fatalf("expired successful merge did not replay: %d %v", revision, err)
	}
	if _, err = s.MergeMusicAlbumMetadata(ctx, "other", a.ID, 8, next, []string{"genre"}, "reject-foreign-merge"); err == nil {
		t.Fatal("merge crossed owner boundary")
	}
}

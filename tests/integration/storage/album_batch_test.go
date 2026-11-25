package storage_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type batchAlbumCatalog struct{}

type singleAlbumPages struct{ music.Repository }

func (s singleAlbumPages) MusicAlbums(ctx context.Context, owner string, q music.Query) (music.AlbumPage, error) {
	q.Limit = 1
	return s.Repository.MusicAlbums(ctx, owner, q)
}

func (batchAlbumCatalog) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{Type: "scraper", PluginID: "musicfs.scraper.musicbrainz", Name: "Test catalog",
		Fields: []string{"albumDetails", "albumTracks"}, Available: true}
}

func (batchAlbumCatalog) Search(_ context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if field == "albumDetails" {
		switch q.Album {
		case "Batch valid":
			if q.Artist != "Singer" {
				return nil, nil
			}
		case "Batch unavailable":
			return nil, domain.Fail("SOURCE_OFFLINE", "Album catalog is offline")
		case "Batch unmatched":
			return nil, nil
		}
		return []scraper.Item{{ID: "incomplete", Title: q.Album}, {ID: "wrong-artist", Title: q.Album}, {ID: "complete", Title: q.Album}}, nil
	}
	id := q.ExternalID
	if id == "incomplete" {
		id = "different-release"
	}
	artist := "Singer"
	if id == "wrong-artist" {
		artist = "Unrelated singer"
	}
	return []scraper.Item{{ID: id, Title: "Edition", Tracks: []scraper.Track{{Number: 1, Title: "First track", Artist: artist}}}}, nil
}

func TestAlbumBatchScopesFallsBackAndProtectsConcurrentEdits(t *testing.T) {
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	service, err := onlinescrape.NewWithSettings([]scraper.Plugin{batchAlbumCatalog{}}, store)
	if err != nil {
		t.Fatal(err)
	}
	rev := int64(0)
	_, err = service.SaveAlbumAutoSettings(ctx, "admin", onlinescrape.AlbumAutoSettingsInput{
		ExpectedRevision: &rev, LocalPluginOrder: []string{batchAlbumCatalog{}.Descriptor().PluginID},
	}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	create := func(owner, name string) music.Album {
		t.Helper()
		album, err := store.SaveMusicAlbum(ctx, owner, "", 0, music.AlbumFields{Name: name}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		return album
	}
	saveManual := func(album music.Album) {
		t.Helper()
		_, err := store.SelectMusicAlbumScrapedField(ctx, "admin", album.ID, album.Revision, works.ScrapedField{
			Field: "albumDetails", PluginID: "scraper.manual", CandidateToken: domain.NewID(), Item: scraper.Item{ID: "manual", Title: "Selected release"},
		}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
	}
	var observations []library.Observation
	for _, name := range []string{"Batch valid", "Batch edited", "Batch unavailable", "Batch wrong title", "Batch unmatched"} {
		obs := visibilityObservation(name, name+".wav", "Singer", name, "Jazz", "wav")
		title := "First track"
		if name == "Batch wrong title" {
			title = "A different recording"
		}
		obs.Title = &title
		observations = append(observations, obs)
	}
	albums := map[string]music.Album{}
	for _, source := range visibilityScan(t, store, "admin", "local", observations...) {
		track, err := store.MusicTrackForSource(ctx, "admin", source.ID)
		if err != nil || len(track.Albums) != 1 {
			t.Fatalf("source album: %+v, %v", track.Albums, err)
		}
		albums[track.Albums[0].Name] = track.Albums[0]
	}
	success, changed := albums["Batch valid"], albums["Batch edited"]
	saved := create("admin", "Batch previously saved")
	saveManual(saved)
	empty := create("admin", "Batch empty")
	outside := create("admin", "Other album")
	private := create("other", "Batch valid")
	var reports []onlinescrape.AlbumBatchProgress
	result, err := service.AutoScrapeAlbums(ctx, singleAlbumPages{store}, "admin", "", "Batch", func(p onlinescrape.AlbumBatchProgress) error {
		reports = append(reports, p)
		if p.Processed == 0 {
			saveManual(changed)
		}
		return nil
	})
	var failure *domain.Error
	want := onlinescrape.AlbumBatchProgress{Total: 6, Processed: 6, Saved: 1, Skipped: 1, NotFound: 3, Failed: 1}
	if !errors.As(err, &failure) || failure.Code != "SOURCE_OFFLINE" || result != want || len(reports) != want.Total+1 || reports[len(reports)-1] != want {
		t.Fatalf("batch result %+v, reports %+v, error %v", result, reports, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	result, err = service.AutoScrapeAlbums(cancelled, store, "admin", "", "Other album", func(onlinescrape.AlbumBatchProgress) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || result.Total != 1 || result.Processed != 0 {
		t.Fatalf("cancelled batch: %+v, %v", result, err)
	}
	for _, check := range []struct {
		owner, album, release string
		count                 int
	}{
		{"admin", success.ID, "complete", 2}, {"admin", changed.ID, "manual", 1}, {"admin", saved.ID, "manual", 1},
		{"admin", outside.ID, "", 0}, {"other", private.ID, "", 0}, {"admin", empty.ID, "", 0},
		{"admin", albums["Batch wrong title"].ID, "", 0},
	} {
		fields, err := store.MusicAlbumScrapedFields(ctx, check.owner, check.album)
		if err != nil || len(fields) != check.count {
			t.Fatalf("saved fields in %s: %+v, %v", check.album, fields, err)
		}
		for _, field := range fields {
			if field.Item.ID != check.release {
				t.Fatalf("unexpected edition in %s: %+v", check.album, field)
			}
		}
	}
}

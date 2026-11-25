package storage_test

import (
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

func localizedObservation(id, title string, artists []string, duration int64) library.Observation {
	o := visibilityObservation(id, id+".wav", "Singer", "Album", "pop", "wav")
	o.Title, o.Artists, o.DurationMs = &title, artists, &duration
	return o
}

func applyAlbumTracks(t *testing.T, s *sqlite.Store, id string, tracks []scraper.Track) {
	t.Helper()
	album, err := s.MusicAlbum(t.Context(), "admin", id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectMusicAlbumScrapedFields(t.Context(), "admin", id, album.Revision, albumBundleFields(tracks), domain.NewID()); err != nil {
		t.Fatal(err)
	}
}

func albumRows(t *testing.T, s *sqlite.Store, id, q string) []music.Track {
	t.Helper()
	page, err := s.MusicTracks(t.Context(), "admin", music.Query{AlbumID: id, Limit: 100, Q: q})
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func TestScrapedAlbumReconcilesLocalizedTitlesWithoutGuessingLanguage(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	base := localizedObservation("existing", "Local track", []string{"Local singer"}, 143000)
	source := visibilityScan(t, s, "admin", "local", base)[0]
	m, err := s.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := m.Albums[0].ID
	artist := m.Artists[0]
	if _, err = s.SaveMusicArtistNames(ctx, "admin", artist.ID, artist.Revision, music.ArtistNamesInput{LocalizedNames: []scraper.LocalizedTitle{{Language: "ja", Title: "歌手"}}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	tracks := []scraper.Track{{Title: "Another Name", Artist: "歌手", Duration: 143.11, Disc: 1, Number: 1, ExternalID: "first"},
		{Title: "New track", Artist: "Other artist", Duration: 190, Disc: 1, Number: 2, ExternalID: "second"}}
	applyAlbumTracks(t, s, album, tracks)
	rows := albumRows(t, s, album, "")
	alias := scraper.LocalizedTitle{Language: "und", Title: "Another Name"}
	if len(rows) != 2 || rows[0].ID != m.ID || rows[0].Title != "Local track" || len(rows[0].Sources) != 1 || !slices.Contains(rows[0].LocalizedTitles, alias) {
		t.Fatalf("incorrect localized matching: %+v", rows)
	}
	if got := albumRows(t, s, album, "another name"); len(got) != 1 || got[0].ID != m.ID {
		t.Fatalf("alias did not resolve track: %+v", got)
	}
	if got := albumRows(t, s, album, "歌手"); len(got) != 1 || got[0].ID != m.ID {
		t.Fatalf("artist alias did not resolve track: %+v", got)
	}
	// 同一平台的曲目编号不随盘内位置变化。
	tracks[0], tracks[1] = tracks[1], tracks[0]
	tracks[0].Number, tracks[1].Number = 1, 2
	applyAlbumTracks(t, s, album, tracks)
	updated := albumRows(t, s, album, "")
	if len(updated) != 2 || updated[1].ID != m.ID || updated[0].ID != rows[1].ID {
		t.Fatalf("release-track identities were lost: %+v", updated)
	}
	// 旧的资料占位在本地音源出现后可重新匹配，保留音源本身的显示名。
	later := localizedObservation("added", "本地新增", []string{"Other artist"}, 190100)
	visibilityScan(t, s, "admin", "local", base, later)
	applyAlbumTracks(t, s, album, tracks)
	updated = albumRows(t, s, album, "")
	if len(updated) != 2 || updated[0].Title != "本地新增" || len(updated[0].Sources) != 1 || updated[1].ID != m.ID {
		t.Fatalf("rescrape retained a disposable placeholder: %+v", updated)
	}
	later.ObservedVersion = "v2"
	reloaded := visibilityScan(t, s, "admin", "local", base, later)[1]
	real, err := s.MusicTrackForSource(ctx, "admin", reloaded.ID)
	if err != nil || real.ID != updated[0].ID || !slices.Equal(real.AlbumIDs, []string{album}) {
		t.Fatalf("rescanned source lost its matched album: %+v, %v", real, err)
	}
}

func TestScrapedAlbumRejectsAmbiguousDurationAndCredits(t *testing.T) {
	s := visibilityStore(t)
	obs := []library.Observation{
		localizedObservation("a", "First local", []string{"A", "B"}, 200000),
		localizedObservation("b", "Second local", []string{"A", "B"}, 200500),
		localizedObservation("c", "Third local", []string{"C"}, 250000),
	}
	source := visibilityScan(t, s, "admin", "local", obs...)[0]
	m, err := s.MusicTrackForSource(t.Context(), "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := m.Albums[0].ID
	applyAlbumTracks(t, s, album, []scraper.Track{
		{Title: "Two local candidates", Artist: "A×B", Duration: 200.2, Number: 1},
		{Title: "Same length one", Artist: "C", Duration: 250.1, Number: 2},
		{Title: "Same length two", Artist: "C", Duration: 250.2, Number: 3},
		{Title: "Different artist", Artist: "D", Duration: 250, Number: 4},
	})
	rows := albumRows(t, s, album, "")
	if len(rows) != 7 {
		t.Fatalf("ambiguous evidence was accepted: %+v", rows)
	}
	for _, row := range rows[:4] {
		if len(row.Sources) != 0 {
			t.Fatal("incorrect source binding", row.Title)
		}
	}
}

func TestScrapedAlbumMatchesCompleteLocalizedArtistCredit(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	sources := visibilityScan(t, s, "admin", "local",
		localizedObservation("duet", "本地曲名", []string{"北", "南"}, 180800),
		localizedObservation("solo", "另一首", []string{"北"}, 180800))
	duet, err := s.MusicTrackForSource(ctx, "admin", sources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	album := duet.Albums[0].ID
	applyAlbumTracks(t, s, album, []scraper.Track{{Title: "Another language", Artist: "North & South", Duration: 180.8, Number: 1,
		LocalizedArtistNames: []scraper.LocalizedTitle{{Language: "zh", Title: "北 & 南"}}}})
	got := albumRows(t, s, album, "Another language")
	if len(got) != 1 || got[0].ID != duet.ID || got[0].Title != duet.Title || len(got[0].Artists) != 2 || len(got[0].Sources) != 1 {
		t.Fatalf("failed to match complete translated credit: %+v", got)
	}
	for _, artist := range got[0].Artists {
		if len(artist.LocalizedNames) > 0 {
			t.Fatalf("stored a group credit on an individual artist: %+v", artist)
		}
	}
}

func TestNewAlbumTracksReuseUniqueArtistAliases(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Album"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	var artists []music.Artist
	for _, name := range []string{"歌手甲", "歌手乙"} {
		track, err := s.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{
			TrackFields: music.TrackFields{Title: name}, ArtistNames: []string{name},
		}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		names := []scraper.LocalizedTitle{{Language: "en", Title: "Shared alias"}}
		if len(artists) == 0 {
			names = append(names, scraper.LocalizedTitle{Language: "en", Title: "First artist"})
		}
		artist, err := s.SaveMusicArtistNames(ctx, "admin", track.Artists[0].ID, track.Artists[0].Revision,
			music.ArtistNamesInput{LocalizedNames: names}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		artists = append(artists, artist)
	}
	applyAlbumTracks(t, s, album.ID, []scraper.Track{
		{Title: "First song", Artist: "First artist", Number: 1},
		{Title: "Second song", Artist: "Shared alias", Number: 2},
	})
	rows := albumRows(t, s, album.ID, "")
	if len(rows) != 2 || len(rows[0].Artists) != 1 || rows[0].Artists[0].ID != artists[0].ID ||
		len(rows[1].Artists) != 1 || rows[1].Artists[0].Name != "Shared alias" {
		t.Fatalf("incorrect credits for localized artist names: %+v", rows)
	}
}

package storage_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
)

func TestOrdinaryMusicCatalogExcludesPackageCategoriesFacetsAndAlbums(t *testing.T) {
	s := visibilityStore(t)
	service := catalog.New(s)
	keep := visibilityObservation("keep", "common/regular/keep.wav", "Shared Artist", "Music Album", "Rock", "wav")
	asmr := visibilityObservation("asmr", "common/asmr/voice.wav", "Voice Actor", "Voice Album", "Whisper", "mp3")
	cd := visibilityObservation("cd", "common/disc/disc.wav", "Shared Artist", "Disc Album", "Rock", "flac")
	voiceCD := visibilityObservation("voice-cd", "common/voice-disc/unknown.wav", "Unknown Actor", "Unknown Album", "Unknown Genre", "aac")
	voiceCD.Year, voiceCD.SampleRateHz = nil, nil
	tracks := visibilityScan(t, s, "admin", "local", keep, asmr, cd, voiceCD)
	hiddenAlbum := albumCreate(t, s, "admin", "local", "Hidden source album", tracks[1].ID, tracks[2].ID)
	mixedAlbum := albumCreate(t, s, "admin", "local", "Mixed source album", tracks[0].ID, tracks[1].ID)
	emptyAlbum := albumCreate(t, s, "admin", "local", "Empty source album")
	before, err := service.Search(t.Context(), "admin", catalog.Query{}, "", 1)
	if err != nil || before.Total != 4 {
		t.Fatal(before, err)
	}
	beforeArtists, err := service.Search(t.Context(), "admin", catalog.Query{Type: "artists", Q: "Voice Actor"}, "", 200)
	if err != nil || beforeArtists.Total != 1 {
		t.Fatal(beforeArtists, err)
	}
	var hiddenArtist catalog.Artist
	if err = json.Unmarshal(beforeArtists.Items[0], &hiddenArtist); err != nil {
		t.Fatal(err)
	}
	oldGenres := categorySearch(t, service, catalog.Query{CategoryKind: "genre"})
	whisper := categoryFind(t, oldGenres, "", "Whisper", false)
	visibilityPackage(t, s, "admin", "local", "music", "directory", keep)
	visibilityPackage(t, s, "admin", "local", "asmr", "directory", asmr)
	visibilityPackage(t, s, "admin", "local", "music", "cd_dump", cd)
	visibilityPackage(t, s, "admin", "local", "asmr", "cd_dump", voiceCD)
	current, err := service.Search(t.Context(), "admin", catalog.Query{}, "", 200)
	if err != nil || current.Total != 1 || current.CatalogRevision == before.CatalogRevision {
		t.Fatal(current, err)
	}
	var visible catalog.Track
	if err = json.Unmarshal(current.Items[0], &visible); err != nil || visible.ID != tracks[0].ID {
		t.Fatal(visible, err)
	}
	// 已发布快照保持固定；刷新后的查询必须采用新归属。
	stable, err := service.Page(t.Context(), "admin", before.QueryID, "", 200)
	if err != nil || stable.Total != 4 || stable.CatalogRevision != before.CatalogRevision {
		t.Fatal(stable, err)
	}
	for _, query := range []catalog.Query{{Q: "Voice"}, {Genres: []string{"Whisper"}}, {Formats: []string{"mp3"}}, {SourceAlbumID: hiddenAlbum.ID}, {ArtistID: hiddenArtist.ID}, {UnknownFields: []string{"year"}}} {
		page, err := service.Search(t.Context(), "admin", query, "", 200)
		if err != nil || page.Total != 0 {
			t.Fatalf("hidden filter returned items: %+v %+v %v", query, page, err)
		}
	}
	_, err = service.Artist(t.Context(), "admin", hiddenArtist.ID)
	matchingError(t, err, "NOT_FOUND")
	_, err = service.Search(t.Context(), "admin", catalog.Query{CategoryKind: "genre", CategoryID: whisper.ID}, "", 200)
	matchingError(t, err, "NOT_FOUND")
	for _, test := range []struct{ kind, name, provider string }{
		{"artist", "Shared Artist", "local"}, {"album", "Music Album", "local"}, {"genre", "Rock", ""},
	} {
		categories := categorySearch(t, service, catalog.Query{CategoryKind: test.kind})
		if len(categories) != 1 || categoryFind(t, categories, test.provider, test.name, false).TrackCount != 1 {
			t.Fatalf("hidden %s category remained: %+v", test.kind, categories)
		}
	}
	facets, err := service.Facets(t.Context(), "admin", "local")
	if err != nil || len(facets.Formats) != 1 || facets.Formats[0].Name != "wav" || facets.Formats[0].TrackCount != 1 ||
		len(facets.Genres) != 1 || facets.Genres[0].Value != "rock" || facets.Genres[0].TrackCount != 1 ||
		len(facets.Years) != 1 || facets.Years[0].TrackCount != 1 || facets.UnknownCounts["year"] != 0 ||
		len(facets.SampleRatesHz) != 1 || facets.SampleRatesHz[0].TrackCount != 1 || facets.UnknownCounts["sampleRateHz"] != 0 {
		t.Fatal("hidden facet remained", facets, err)
	}
	if len(facets.Directories) != 3 {
		t.Fatal("hidden directory remained", facets.Directories)
	}
	for _, directory := range facets.Directories {
		if directory.TrackCount != 1 || directory.Name != "Root" && directory.Name != "common" && directory.Name != "regular" {
			t.Fatal("shared ancestor count includes hidden sources", directory)
		}
		categories := categorySearch(t, service, catalog.Query{CategoryKind: "folder", ParentID: directory.ID})
		for _, category := range categories {
			if category.TrackCount != 1 || category.Name != "common" && category.Name != "regular" {
				t.Fatal("hidden folder category remained", category)
			}
		}
	}
	artists, err := service.Search(t.Context(), "admin", catalog.Query{Type: "artists"}, "", 200)
	if err != nil || artists.Total != 1 {
		t.Fatal(artists, err)
	}
	var artist catalog.Artist
	if err = json.Unmarshal(artists.Items[0], &artist); err != nil || artist.Name != "Shared Artist" || artist.TrackCount != 1 || artist.AlbumCount != 1 {
		t.Fatal(artist, err)
	}
	albums, err := service.Search(t.Context(), "admin", catalog.Query{Type: "albums"}, "", 200)
	if err != nil || albums.Total != 2 {
		t.Fatal(albums, err)
	}
	for _, raw := range albums.Items {
		var album catalog.Album
		if err = json.Unmarshal(raw, &album); err != nil {
			t.Fatal(err)
		}
		if album.ID == mixedAlbum.ID {
			if album.TrackCount != 1 || !reflect.DeepEqual(album.Artists, []string{"Shared Artist"}) {
				t.Fatal("mixed album includes hidden metadata", album)
			}
		} else if album.ID != emptyAlbum.ID || album.TrackCount != 0 {
			t.Fatal("hidden source album remained", album)
		}
	}
	legacy, err := s.SourceAlbums(t.Context(), "admin", "local", "", 100)
	if err != nil || len(legacy) != 2 {
		t.Fatal(legacy, err)
	}
	for _, album := range legacy {
		if album.ID == mixedAlbum.ID && !reflect.DeepEqual(album.SourceTrackIDs, []string{tracks[0].ID}) || album.ID == hiddenAlbum.ID {
			t.Fatal("legacy album contains hidden source", album)
		}
	}
	rawAlbum, err := s.SourceAlbum(t.Context(), "admin", hiddenAlbum.ID)
	if err != nil || len(rawAlbum.SourceTrackIDs) != 2 {
		t.Fatal("source album data was removed", rawAlbum, err)
	}
}

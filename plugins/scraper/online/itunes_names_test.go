package online

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func TestITunesJapaneseAlbumSearchAndPageLookup(t *testing.T) {
	client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("country") != "JP" {
			fmt.Fprint(w, `{"results":[]}`)
			return
		}
		name := "Fireworks"
		if q.Get("lang") == "ja_jp" {
			name = "打上花火"
		}
		switch r.URL.Path {
		case "/search":
			if q.Get("country") != "JP" || q.Get("lang") != "ja_jp" {
				t.Errorf("unexpected Japanese search language: %s", r.URL)
			}
			fmt.Fprint(w, `{"results":[{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Fireworks"}]}`)
		case "/lookup":
			if q.Get("id") != "23" || q.Get("entity") != "song" || q.Get("limit") != "1" && q.Get("limit") != "200" {
				t.Errorf("unexpected album lookup %s", r.URL)
			}
			fmt.Fprintf(w, `{"results":[
				{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Fireworks","trackCount":1},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":50,"trackName":%q,"collectionName":%q}]}`, name, name)
		default:
			t.Errorf("unexpected URL %s", r.URL)
		}
	})
	for _, tc := range []struct {
		field string
		query scraper.Query
	}{
		{"albumDetails", scraper.Query{Album: "Fireworks", Country: "jp"}},
		{"albumDetails", scraper.Query{SourceURL: "https://music.apple.com/jp/album/23"}},
		{"albumTracks", scraper.Query{ExternalID: "23", Country: "JP"}},
	} {
		items, err := ITunes(client).Search(t.Context(), tc.field, tc.query)
		if err != nil || len(items) != 1 || items[0].Title != "打上花火" || items[0].Album != items[0].Title {
			t.Fatalf("unexpected Japanese album result: %#v %v", items, err)
		}
		if tc.field == "albumTracks" && (len(items[0].Tracks) != 1 || items[0].Tracks[0].Title != "打上花火") {
			t.Fatalf("unexpected Japanese track list: %#v", items[0].Tracks)
		}
	}
}

func TestITunesBatchesJapaneseAlbumSearchNamesAndKeepsFailedResults(t *testing.T) {
	for _, failLookup := range []bool{false, true} {
		searches, lookups := 0, 0
		client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("country") != "JP" || q.Get("lang") != "ja_jp" {
				t.Errorf("unexpected storefront or language: %s", r.URL)
			}
			switch r.URL.Path {
			case "/search":
				searches++
				fmt.Fprint(w, `{"results":[
					{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Fireworks"},
					{"wrapperType":"collection","collectionType":"Album","collectionId":40,"collectionName":"Moon"},
					{"wrapperType":"collection","collectionType":"Album","collectionId":46,"collectionName":"Sea"},
					{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Fireworks"}]}`)
			case "/lookup":
				lookups++
				if q.Get("id") != "23,40,46" || q.Get("entity") != "song" || q.Get("limit") != "1" {
					t.Errorf("unexpected batch lookup: %s", r.URL)
				}
				if failLookup {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				fmt.Fprint(w, `{"results":[
					{"wrapperType":"track","kind":"song","collectionId":40,"collectionName":"月"},
					{"wrapperType":"track","kind":"song","collectionId":99,"collectionName":"Another edition"},
					{"wrapperType":"track","kind":"song","collectionId":23,"collectionName":"打上花火"}]}`)
			default:
				t.Errorf("unexpected URL %s", r.URL)
			}
		})
		items, err := ITunes(client).Search(t.Context(), "albumDetails", scraper.Query{Album: "Test", Country: "JP"})
		want := []string{"打上花火", "月", "Sea", "打上花火"}
		if failLookup {
			want = []string{"Fireworks", "Moon", "Sea", "Fireworks"}
		}
		if err != nil || len(items) != len(want) || searches != 1 || lookups != 1 {
			t.Fatalf("unexpected album search: %d items, %d searches, %d lookups, error: %v", len(items), searches, lookups, err)
		}
		for i, item := range items {
			if item.Title != want[i] || item.Album != want[i] {
				t.Errorf("unexpected name for %s: title=%q album=%q, want %q", item.ID, item.Title, item.Album, want[i])
			}
		}
	}
}

func TestJapaneseCollectionNameUsesConsistentSongRowsOfSameEdition(t *testing.T) {
	album := itunesEntry{CollectionID: 23, CollectionName: "Fireworks"}
	song := itunesEntry{WrapperType: "track", Kind: "song", CollectionID: 23, CollectionName: "打上花火"}
	otherEdition, video, conflict, blank := song, song, song, song
	otherEdition.CollectionID, video.Kind = 24, "music-video"
	conflict.CollectionName, blank.CollectionName = "Another title", ""
	for _, tc := range []struct {
		rows []itunesEntry
		want string
	}{
		{[]itunesEntry{album, otherEdition, video}, "Fireworks"},
		{[]itunesEntry{song, blank}, "Fireworks"},
		{[]itunesEntry{song, conflict}, "Fireworks"},
		{[]itunesEntry{song, song, otherEdition, video}, "打上花火"},
	} {
		if got := japaneseCollectionName(album, tc.rows); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestITunesAlbumNamesJoinByBothAppleIDs(t *testing.T) {
	var requests int
	client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if r.URL.Path != "/lookup" || q.Get("id") != "23" || q.Get("entity") != "song" {
			t.Errorf("unexpected lookup %s", r.URL)
		}
		if q.Get("country") != "JP" && q.Has("lang") {
			t.Errorf("unexpected language outside the Japanese storefront: %s", r.URL)
		}
		switch q.Get("country") {
		case "US":
			fmt.Fprint(w, `{"results":[
				{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Fireworks","trackCount":2},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":50,"trackName":"Uchiagehanabi","artistName":"DAOKO & Kenshi Yonezu","trackNumber":1},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":51,"trackName":"Forever Friends","artistName":"DAOKO","trackNumber":2}]}`)
		case "JP":
			if q.Get("lang") != "ja_jp" {
				fmt.Fprint(w, `{"results":[{"wrapperType":"track","kind":"song","collectionId":23,"trackId":50,"trackName":"Uchiagehanabi","artistName":"DAOKO & Kenshi Yonezu"}]}`)
				return
			}
			fmt.Fprint(w, `{"results":[
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":51,"trackName":"Forever Friends","artistName":"DAOKO"},
				{"wrapperType":"track","kind":"song","collectionId":24,"trackId":50,"trackName":"Another edition","artistName":"Other edition guest"},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":110,"trackName":"Unknown track","artistName":"Other track guest"},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":50,"trackName":"打上花火","artistName":"DAOKO×米津玄師"}]}`)
		case "TW":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected storefront %s", q.Get("country"))
		}
	})
	items, err := ITunes(client).Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "23"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 {
		t.Fatalf("expected a complete album: %#v %v", items, err)
	}
	first, second := items[0].Tracks[0], items[0].Tracks[1]
	want := []scraper.LocalizedTitle{{Language: "und", Title: "打上花火"}}
	if requests != 3 || first.Title != "Uchiagehanabi" || first.ExternalID != "50" || !reflect.DeepEqual(first.LocalizedTitles, want) || len(second.LocalizedTitles) != 0 {
		t.Fatalf("incorrect cross-store titles: %#v (%d requests)", items, requests)
	}
	wantCredit := []scraper.LocalizedTitle{{Language: "und", Title: "DAOKO×米津玄師"}}
	if first.Artist != "DAOKO & Kenshi Yonezu" || !reflect.DeepEqual(first.LocalizedArtistNames, wantCredit) || len(second.LocalizedArtistNames) != 0 || len(items[0].LocalizedArtistNames) != 0 {
		t.Fatalf("incorrect cross-store artist credits: %#v", items)
	}
}

func TestITunesLocalizesAllArtistSearchResultsInOneBatchPerStore(t *testing.T) {
	var requests int
	client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if r.URL.Path == "/search" {
			fmt.Fprint(w, `{"results":[{"wrapperType":"artist","artistId":31,"artistName":"Singer One"},{"wrapperType":"artist","artistId":32,"artistName":"Singer Two"}]}`)
			return
		}
		if q.Get("id") != "31,32" {
			t.Errorf("unbatched artist lookup %s", r.URL)
		}
		fmt.Fprint(w, `{"results":[{"wrapperType":"track","kind":"song","artistId":31,"artistName":"Singer One & Singer Two"},{"wrapperType":"artist","artistId":32,"artistName":"歌手二"},{"wrapperType":"artist","artistId":31,"artistName":"歌手一"}]}`)
	})
	items, err := ITunes(client).Search(t.Context(), "artistDetails", scraper.Query{Artist: "Singer"})
	if err != nil || len(items) != 2 || requests != 3 {
		t.Fatalf("artist lookup failed: %v (%d requests)", err, requests)
	}
	for index, title := range []string{"歌手一", "歌手二"} {
		want := []scraper.LocalizedTitle{{Language: "und", Title: title}}
		if !reflect.DeepEqual(items[index].LocalizedArtistNames, want) {
			t.Errorf("incorrect aliases on artist %s: %#v", items[index].ID, items[index].LocalizedArtistNames)
		}
	}
}

func TestITunesTrackDetailsKeepOnlyNamesForTheSameEdition(t *testing.T) {
	client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			fmt.Fprint(w, `{"results":[{"wrapperType":"track","kind":"song","trackId":50,"collectionId":23,"trackName":"Uchiagehanabi"}]}`)
			return
		}
		if r.URL.Query().Get("id") != "50" {
			t.Errorf("track details lookup has the wrong id: %s", r.URL)
		}
		fmt.Fprint(w, `{"results":[{"wrapperType":"track","kind":"song","trackId":50,"collectionId":25,"trackName":"Live"},{"wrapperType":"track","kind":"song","trackId":50,"collectionId":23,"trackName":"打上花火"}]}`)
	})
	items, err := ITunes(client).Search(t.Context(), "trackDetails", scraper.Query{Title: "Uchiagehanabi"})
	want := []scraper.LocalizedTitle{{Language: "und", Title: "打上花火"}}
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0].LocalizedTitles, want) {
		t.Fatalf("incorrect song lookup: %#v %v", items, err)
	}
}

package online

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func TestITunesAlbumNamesJoinByBothAppleIDs(t *testing.T) {
	var requests int
	client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		if r.URL.Path != "/lookup" || q.Get("id") != "23" || q.Get("entity") != "song" {
			t.Errorf("unexpected lookup %s", r.URL)
		}
		switch q.Get("country") {
		case "US":
			fmt.Fprint(w, `{"results":[
				{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Fireworks","trackCount":2},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":50,"trackName":"Uchiagehanabi","artistName":"DAOKO & Kenshi Yonezu","trackNumber":1},
				{"wrapperType":"track","kind":"song","collectionId":23,"trackId":51,"trackName":"Forever Friends","artistName":"DAOKO","trackNumber":2}]}`)
		case "JP":
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

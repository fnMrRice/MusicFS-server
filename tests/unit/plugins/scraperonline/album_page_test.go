package scraperonline_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

func TestAlbumPageLookupUsesPinnedCatalogAndCompleteTracks(t *testing.T) {
	for _, tc := range []struct {
		name, page, host string
		plugin           func(*http.Client) scraper.Plugin
	}{
		{"MusicBrainz", "https://musicbrainz.org/release/" + releaseID, "musicbrainz.org", online.MusicBrainz},
		{"Apple Music", "https://music.apple.com/hk/album/example/23?i=24", "itunes.apple.com", online.ITunes},
		{"iTunes", "https://itunes.apple.com/tw/album/example/id23", "itunes.apple.com", online.ITunes},
		{"Bangumi", "https://bgm.tv/subject/23", "api.bgm.tv", online.Bangumi},
		{"Bangumi alternate", "https://chii.in/subject/23/", "api.bgm.tv", online.Bangumi},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := catalogClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Host != tc.host || r.URL.Scheme != "https" {
					t.Fatalf("album page was fetched as an arbitrary endpoint: %s", r.URL)
				}
				switch tc.host {
				case "musicbrainz.org":
					if r.URL.Path != "/ws/2/release/"+releaseID {
						t.Fatal("incorrect release lookup", r.URL)
					}
					fmt.Fprintf(w, `{"id":"%s","title":"Album","media":[{"position":1,"track-count":2,"tracks":[{"position":1,"title":"First"},{"position":2,"title":"Second"}]}]}`, releaseID)
				case "itunes.apple.com":
					country := "HK"
					if tc.name == "iTunes" {
						country = "TW"
					}
					if r.URL.Path != "/lookup" || r.URL.Query().Get("id") != "23" || r.URL.Query().Get("country") != country || r.URL.Query().Get("entity") != "song" {
						t.Fatal("album page region or identity was lost", r.URL)
					}
					fmt.Fprint(w, `{"results":[{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Album","trackCount":2,"primaryGenreName":"Mandopop","artworkUrl100":"https://is1-ssl.mzstatic.com/image/thumb/cover.jpg"},{"wrapperType":"track","kind":"song","collectionId":23,"trackName":"First","discNumber":1,"trackNumber":1},{"wrapperType":"track","kind":"song","collectionId":23,"trackName":"Second","discNumber":1,"trackNumber":2}]}`)
				case "api.bgm.tv":
					if r.URL.Path == "/v0/subjects/23" {
						fmt.Fprint(w, `{"id":23,"name":"Album","type":3}`)
					} else if r.URL.Path == "/v0/episodes" && r.URL.Query().Get("subject_id") == "23" {
						fmt.Fprint(w, `{"total":2,"data":[{"name":"First","disc":1,"sort":1},{"name":"Second","disc":1,"sort":2}]}`)
					} else {
						t.Fatal("incorrect subject lookup", r.URL)
					}
				}
			})
			p := tc.plugin(client)
			service, err := onlinescrape.New([]scraper.Plugin{p})
			if err != nil {
				t.Fatal(err)
			}
			items, err := service.Search(t.Context(), "owner", p.Descriptor().PluginID, "albumTracks", scraper.Query{SourceURL: tc.page})
			if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 || items[0].Tracks[1].Title != "Second" {
				t.Fatalf("complete album lookup: %+v %v", items, err)
			}
			if tc.host == "itunes.apple.com" && (items[0].Genre != "Mandopop" || items[0].ImageURL == "" || items[0].Description != "") {
				t.Fatalf("genre and official artwork were not identified: %+v", items[0].Item)
			}
		})
	}
}

func TestAlbumPageLookupRejectsForeignAndAmbiguousPagesWithoutRequests(t *testing.T) {
	for _, page := range []string{
		"http://musicbrainz.org/release/" + releaseID,
		"https://musicbrainz.org.evil.example/release/" + releaseID,
		"https://user@musicbrainz.org/release/" + releaseID,
		"https://musicbrainz.org:444/release/" + releaseID,
		"https://musicbrainz.org/release-group/" + releaseID,
		"https://musicbrainz.org/recording/" + releaseID,
		"https://127.0.0.1/release/" + releaseID,
	} {
		client := catalogClient(t, func(http.ResponseWriter, *http.Request) { t.Fatal("invalid page caused an upstream request") })
		if _, err := online.MusicBrainz(client).Search(context.Background(), "albumDetails", scraper.Query{SourceURL: page}); err == nil {
			t.Fatalf("invalid album page accepted: %s", page)
		}
	}
	client := catalogClient(t, func(http.ResponseWriter, *http.Request) { t.Fatal("ambiguous page caused an upstream request") })
	for _, q := range []scraper.Query{{SourceURL: "https://music.apple.com/us/artist/example/23"}, {SourceURL: "https://music.apple.com/us/album/example/23", ExternalID: "24"}} {
		if _, err := online.ITunes(client).Search(t.Context(), "albumDetails", q); err == nil {
			t.Fatal("non-album or conflicting page identity was accepted")
		}
	}
	for _, tc := range []struct {
		plugin   scraper.Plugin
		page, id string
	}{
		{online.NetEase(client), "https://music.163.com/album?id=12&id=23", ""},
		{online.NetEase(client), "https://music.163.com/album?id=12#/album?id=23", ""},
		{online.NetEase(client), "https://music.163.com/#/song?id=23", ""},
		{online.NetEase(client), "https://music.163.com.example/album?id=23", ""},
		{online.QQMusic(client), "https://y.qq.com/n/ryqq/albumDetail/0Ab", "0ab"},
		{online.QQMusic(client), "https://y.qq.com/n/ryqq/songDetail/0Ab", ""},
		{online.QQMusic(client), "https://y.qq.com/n/ryqq/albumDetail/x%2Fy", ""},
	} {
		if _, err := tc.plugin.Search(t.Context(), "albumDetails", scraper.Query{SourceURL: tc.page, ExternalID: tc.id}); err == nil {
			t.Fatal("invalid or conflicting album page accepted", tc.page)
		}
	}
}

func TestAlbumTracksRejectIncompleteCatalogResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		plugin func(*http.Client) scraper.Plugin
		id     string
	}{
		{"MusicBrainz", online.MusicBrainz, releaseID},
		{"iTunes", online.ITunes, "23"},
		{"Bangumi", online.Bangumi, "23"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := catalogClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch tc.name {
				case "MusicBrainz":
					fmt.Fprintf(w, `{"id":"%s","title":"Album","media":[{"position":1,"track-count":2,"tracks":[{"position":1,"title":"Only one"}]}]}`, releaseID)
				case "iTunes":
					fmt.Fprint(w, `{"results":[{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Album","trackCount":2},{"wrapperType":"track","kind":"song","collectionId":23,"trackName":"Only one"}]}`)
				case "Bangumi":
					if strings.Contains(r.URL.Path, "/subjects/") {
						fmt.Fprint(w, `{"id":23,"name":"Album","type":3}`)
					} else if r.URL.Query().Get("offset") == "0" {
						fmt.Fprint(w, `{"total":2,"data":[{"name":"Only one","disc":1,"sort":1}]}`)
					} else {
						fmt.Fprint(w, `{"total":2,"data":[]}`)
					}
				}
			})
			items, err := tc.plugin(client).Search(t.Context(), "albumTracks", scraper.Query{ExternalID: tc.id})
			if err == nil || len(items) != 0 {
				t.Fatalf("partial track list was accepted: %+v %v", items, err)
			}
		})
	}
}

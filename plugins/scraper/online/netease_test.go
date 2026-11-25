package online

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func TestNetEaseAlbumSearchAndTrackList(t *testing.T) {
	const album = `{"id":23,"name":"Album","artists":[{"name":"First"},{"name":"Second"}],"publishTime":1286035200000,"size":2,"company":"Label","picUrl":"http://p2.music.126.net/cover.jpg"}`
	client := lookupTestClient(t, "music.163.com", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			if q := r.URL.Query(); q.Get("s") != "Album First" || q.Get("type") != "10" {
				t.Errorf("unexpected search query %s", r.URL)
			}
			if r.URL.Query().Get("limit") == "3" {
				fmt.Fprint(w, `{"code":200,"result":{"albums":[{"id":22,"name":"Removed"},{"id":23,"name":"Summary"},{"id":23,"name":"Summary"}]}}`)
			} else {
				fmt.Fprintf(w, `{"code":200,"result":{"albums":[%s]}}`, album)
			}
		case "/api/v1/album/22":
			fmt.Fprint(w, `{"code":404}`)
		case "/api/v1/album/23":
			fmt.Fprintf(w, `{"code":200,"album":%s,"songs":[
				{"id":31,"name":"One","ar":[{"name":"First"},{"name":"Second"}],"dt":88050,"cd":"1","no":1,"tns":["Alt"]},
				{"id":32,"name":"Two","ar":[{"name":"Second"}],"cd":"2","no":1}]}`, album)
		default:
			t.Errorf("unexpected endpoint %s", r.URL)
			http.NotFound(w, r)
		}
	})
	plugin := NetEase(client)
	for _, field := range []string{"albumDetails", "artwork"} {
		items, err := plugin.Search(t.Context(), field, scraper.Query{Album: "Album", Artist: "First"})
		if err != nil || len(items) != 1 || items[0].ID != "23" || items[0].Artist != "First / Second" ||
			items[0].ImageURL != "https://p2.music.126.net/cover.jpg" || items[0].SourceURL != "https://music.163.com/album?id=23" ||
			items[0].Date != "2010-10-03" || items[0].Publisher != "Label" {
			t.Fatalf("invalid %s candidates %#v: %v", field, items, err)
		}
	}
	items, err := plugin.Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "23"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 {
		t.Fatalf("invalid album tracks %#v: %v", items, err)
	}
	first, second := items[0].Tracks[0], items[0].Tracks[1]
	if first.Title != "One" || first.Duration != 88.05 || first.Artist != "First / Second" || first.ExternalID != "31" || first.Disc != 1 ||
		len(first.LocalizedTitles) != 1 || first.LocalizedTitles[0].Title != "Alt" || second.Disc != 2 || second.Number != 1 {
		t.Errorf("unexpected disc ordering or track metadata %#v", items[0].Tracks)
	}
}

func TestNetEaseIncompleteAlbumCannotBeSelected(t *testing.T) {
	cases := []string{
		`{"code":200,"album":{"id":23,"name":"Album","size":2},"songs":[{"id":1,"name":"One"}]}`,
		`{"code":200,"album":{"id":23,"name":"Album"},"songs":[{"id":1,"name":"One"}]}`,
		`{"code":200,"album":{"id":23,"name":"Album","size":1},"songs":[{"id":1}]}`,
		`{"code":200,"album":{"id":99,"name":"Foreign","size":1},"songs":[{"id":1,"name":"One"}]}`,
		`{"code":200,"album":{"id":23,"name":"Album","size":1},"songs":[{"id":1,"name":"One","al":{"id":99}}]}`,
		`{"code":200,"album":{"id":23,"name":"Album","size":2},"songs":[{"id":1,"name":"One","cd":"1","no":2},{"id":1,"name":"One","cd":"1","no":2}]}`,
	}
	for _, response := range cases {
		plugin := NetEase(lookupTestClient(t, "music.163.com", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		if items, err := plugin.Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "23"}); err == nil || len(items) != 0 {
			t.Errorf("incomplete album emitted %#v: %v", items, err)
		}
	}
}

func TestNetEaseLyricsKeepRecordingIdentity(t *testing.T) {
	client := lookupTestClient(t, "music.163.com", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			if q := r.URL.Query(); q.Get("s") != "Song Singer" || q.Get("type") != "1" || q.Get("limit") != "3" {
				t.Errorf("unexpected song query %s", r.URL)
			}
			fmt.Fprint(w, `{"code":200,"result":{"songs":[{"id":11,"name":"Song"},{"id":12,"name":"Live","album":{"name":"Album"},"artists":[{"name":"Singer"}],"duration":98000},{"id":13,"name":"Other"}]}}`)
		case "/api/v3/song/detail":
			if r.URL.Query().Get("c") != `[{"id":12}]` {
				t.Errorf("unexpected song detail %s", r.URL)
			}
			fmt.Fprint(w, `{"code":200,"songs":[{"id":12,"name":"Live","al":{"name":"Album","picUrl":"https://images.example.com/test"},"ar":[{"name":"Singer"}],"dt":98000}]}`)
		case "/api/song/lyric":
			switch r.URL.Query().Get("id") {
			case "11":
				http.NotFound(w, r)
			case "12":
				fmt.Fprint(w, `{"code":200,"lrc":{"lyric":"[00:01.03]line"}}`)
			case "13":
				fmt.Fprint(w, `{"code":200,"nolyric":true,"lrc":{"lyric":"instrumental"}}`)
			}
		default:
			t.Errorf("unexpected path %s", r.URL)
			http.NotFound(w, r)
		}
	})
	for _, query := range []scraper.Query{{Title: "Song", Artist: "Singer"}, {ExternalID: "12"}} {
		items, err := NetEase(client).Search(t.Context(), "lyrics", query)
		if err != nil || len(items) != 1 || items[0].ID != "12" || items[0].Title != "Live" || items[0].Duration != 98 ||
			items[0].Artist != "Singer" || items[0].Album != "Album" || items[0].Format != "lrc" || items[0].Lyrics != "[00:01.03]line" ||
			items[0].SourceURL != "https://music.163.com/song?id=12" || items[0].ImageURL != "" {
			t.Errorf("unexpected lyrics %#v: %v", items, err)
		}
	}
}

func TestNetEaseArtistImageAndAliases(t *testing.T) {
	plugin := NetEase(lookupTestClient(t, "music.163.com", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/artist/31" {
			t.Errorf("unexpected URL %s", r.URL)
		}
		fmt.Fprint(w, `{"code":200,"artist":{"id":31,"name":"Singer","alias":["Alias"],"picUrl":"https://invalid.test/s.jpg","img1v1Url":"https://p1.music.126.net/s.jpg","briefDesc":"Bio"}}`)
	}))
	for _, field := range []string{"artistImage", "artistDetails"} {
		items, err := plugin.Search(t.Context(), field, scraper.Query{ExternalID: "31"})
		if err != nil || len(items) != 1 || items[0].ImageURL != "https://p1.music.126.net/s.jpg" || items[0].Description != "Bio" ||
			len(items[0].LocalizedArtistNames) != 1 || items[0].LocalizedArtistNames[0].Title != "Alias" {
			t.Errorf("unexpected artist %#v: %v", items, err)
		}
	}
}

func TestNetEaseInstrumentalNotice(t *testing.T) {
	const credit = `{"t":0,"c":[{"tx":"作曲: "},{"tx":"兰音Reine","or":"orpheus\://nm/artist/home?id=46424022"}]}\`
	for _, tc := range []struct {
		name, lyrics string
		wantLyrics   bool
	}{
		{"plain notice", " 纯音乐，请欣赏 ", false},
		{"timed notice and credits", "\ufeff[ti:夜雨]\r\n[ar:兰音Reine][offset:0]\r\n[00:00.00] 作曲 : 兰音Reine\r\n" + credit + "\r\n [00:10][99:00.000] 纯音乐，请欣赏 \r\n[99:59]", false},
		{"structured notice", `{"t":1000,"c":[{"tx":"\u7eaf音乐"},{"tx":"，请欣赏"}]}`, false},
		{"invalid structured line", `{"t":1000,"c":[{"tx":"纯音乐，请欣赏"},{}]}`, true},
		{"notice followed by a verse", "[00:00.00]纯音乐，请欣赏\n[00:10]第一句歌词", true},
		{"structured verse before notice", "{\"t\":0,\"c\":[{\"tx\":\"第一句歌词\"}]}\n[99:00]纯音乐，请欣赏", true},
		{"longer text", "纯音乐，请欣赏这首歌", true},
		{"notice only in metadata", "[ti:纯音乐，请欣赏]\n" + credit, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plugin := NetEase(lookupTestClient(t, "music.163.com", func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/search/get":
					fmt.Fprint(w, `{"code":200,"result":{"songs":[{"id":12,"name":"Song"}]}}`)
				case "/api/v3/song/detail":
					fmt.Fprint(w, `{"code":200,"songs":[{"id":12,"name":"Song"}]}`)
				case "/api/song/lyric":
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "lrc": map[string]string{"lyric": tc.lyrics}})
				default:
					t.Errorf("unexpected endpoint %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			for _, query := range []scraper.Query{{Title: "Song"}, {ExternalID: "12"}} {
				items, err := plugin.Search(t.Context(), "lyrics", query)
				if err != nil || !tc.wantLyrics && len(items) != 0 || tc.wantLyrics && (len(items) != 1 || items[0].Lyrics != tc.lyrics) {
					t.Errorf("unexpected lyrics for query %#v: %#v, %v", query, items, err)
				}
			}
		})
	}
}

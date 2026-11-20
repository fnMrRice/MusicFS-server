package online

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

type lookupTestTransport func(*http.Request) (*http.Response, error)

func (f lookupTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// 用测试服务接收固定站点的请求，正式适配器仍负责拼接路径及参数。
func lookupTestClient(t *testing.T, host string, handler http.HandlerFunc) *http.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := server.Client().Transport
	return &http.Client{Transport: lookupTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != host {
			return nil, fmt.Errorf("unexpected request host %q", r.URL.Host)
		}
		request := r.Clone(r.Context())
		request.URL.Scheme, request.URL.Host = target.Scheme, target.Host
		return transport.RoundTrip(request)
	})}
}

func TestLRCLIBFallsBackToSearch(t *testing.T) {
	client := lookupTestClient(t, "lrclib.net", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("track_name") != "Long Road" || q.Get("artist_name") != "Singer" || q.Get("album_name") != "Concert" {
			t.Errorf("unexpected query %s", r.URL)
		}
		switch r.URL.Path {
		case "/api/get":
			if q.Get("duration") != "214" {
				t.Errorf("unexpected duration %q", q.Get("duration"))
			}
			http.NotFound(w, r)
		case "/api/search":
			if q.Has("duration") {
				t.Errorf("search received duration")
			}
			fmt.Fprint(w, `[{"id":20,"trackName":"Long Road","artistName":"Singer","albumName":"Concert","duration":212,"syncedLyrics":"[00:10]first","plainLyrics":"first"},{"id":21,"trackName":"Long Road","plainLyrics":"plain"},{"id":22,"trackName":"Long Road","instrumental":true}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	items, err := LRCLIB(client).Search(t.Context(), "lyrics", scraper.Query{Title: "Long Road", Artist: "Singer", Album: "Concert", Duration: 213.5})
	if err != nil || len(items) != 2 || items[0].Format != "lrc" || items[0].Lyrics != "[00:10]first" || items[1].Format != "text" {
		t.Fatalf("unexpected lyrics %#v %v", items, err)
	}
}

func TestITunesFindsAlbumAndCompleteStoreTrackList(t *testing.T) {
	client := lookupTestClient(t, "itunes.apple.com", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("country") != "HK" {
			t.Errorf("unexpected country %s", r.URL)
		}
		switch r.URL.Path {
		case "/search":
			if q.Get("entity") != "album" || q.Get("term") != "Example Singer" {
				t.Errorf("unexpected search %s", r.URL)
			}
			fmt.Fprint(w, `{"results":[{"wrapperType":"collection","collectionType":"Album","collectionId":23,"collectionName":"Example","artistName":"Singer","trackCount":2}]}`)
		case "/lookup":
			if q.Get("entity") != "song" || q.Get("id") != "23" {
				t.Errorf("unexpected lookup %s", r.URL)
			}
			fmt.Fprint(w, `{"results":[{"wrapperType":"track","kind":"song","collectionId":23,"trackName":"B","trackNumber":1,"discNumber":2},{"wrapperType":"track","kind":"song","collectionId":23,"trackName":"A","trackNumber":1,"discNumber":1,"trackTimeMillis":75220},{"wrapperType":"track","kind":"song","collectionId":42,"trackName":"Foreign"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	items, err := ITunes(client).Search(t.Context(), "albumTracks", scraper.Query{Album: "Example", Artist: "Singer", Country: "hk"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 || items[0].Tracks[0].Title != "A" || items[0].Tracks[0].Duration != 75.22 || items[0].Tracks[1].Disc != 2 {
		t.Fatalf("unexpected track list %#v %v", items, err)
	}
}

func TestBangumiSeparatesPortraitAndSubjectData(t *testing.T) {
	client := lookupTestClient(t, "api.bgm.tv", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/search/persons":
			var body map[string]any
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || body["keyword"] != "Singer" {
				t.Errorf("invalid artist search")
			}
			fmt.Fprint(w, `{"data":[{"id":31,"name":"Singer","images":{"large":"https://lain.bgm.tv/pic/crt/l/test.jpg"}},{"id":32,"name":"Other","images":{"large":"http://localhost/"}}]}`)
		case "/v0/subjects/23":
			fmt.Fprint(w, `{"id":23,"name":"Example","type":3}`)
		case "/v0/episodes":
			if r.URL.Query().Get("subject_id") != "23" {
				t.Errorf("invalid subject")
			}
			switch r.URL.Query().Get("offset") {
			case "0":
				fmt.Fprint(w, `{"total":2,"data":[{"id":123,"name":"B","name_cn":"乙","disc":2,"sort":1,"duration_seconds":83}]}`)
			case "1":
				fmt.Fprint(w, `{"total":2,"data":[{"name_cn":"A","disc":1,"sort":1}]}`)
			default:
				t.Errorf("unexpected page")
				http.NotFound(w, r)
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	plugin := Bangumi(client)
	items, err := plugin.Search(t.Context(), "artistImage", scraper.Query{Artist: "Singer"})
	if err != nil || len(items) != 1 || items[0].ID != "31" || items[0].ImageURL == "" {
		t.Fatalf("unexpected portrait %#v %v", items, err)
	}
	items, err = plugin.Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "23"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 || items[0].Tracks[0].Title != "A" || items[0].Tracks[1].Duration != 83 {
		t.Fatalf("unexpected tracks %#v %v", items, err)
	}
	track := items[0].Tracks[1]
	if track.ExternalID != "123" || len(track.LocalizedTitles) != 1 || track.LocalizedTitles[0] != (scraper.LocalizedTitle{Language: "zh", Title: "乙"}) {
		t.Fatalf("missing episode identity or translated title: %#v", track)
	}
}

func TestBangumiChapterLayoutProducesValidTrackNames(t *testing.T) {
	client := lookupTestClient(t, "api.bgm.tv", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/2584":
			fmt.Fprint(w, `{"id":2584,"name":"初音ミク ベスト～impacts～","type":3}`)
		case "/v0/episodes":
			fmt.Fprint(w, `{"total":2,"data":[
				{"id":15946,"sort":17,"name":"Special Track:\t卑怯戦隊うろたんだー / シンP feat.MEIKO,KAITO,ミク,リン&amp;レン"},
				{"id":20000,"sort":18,"name":" \r\n ","name_cn":"曲名  A　 B　\t \r\n　副题"}
			]}`)
		default:
			http.NotFound(w, r)
		}
	})
	items, err := Bangumi(client).Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "2584"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 {
		t.Fatalf("unexpected album tracks: %#v %v", items, err)
	}
	first, second := items[0].Tracks[0], items[0].Tracks[1]
	if first.ExternalID != "15946" || first.Title != "Special Track: 卑怯戦隊うろたんだー / シンP feat.MEIKO,KAITO,ミク,リン&amp;レン" {
		t.Errorf("unexpected special track title: %#v", first)
	}
	if second.Title != "曲名  A　 B 副题" || len(second.LocalizedTitles) != 1 ||
		second.LocalizedTitles[0] != (scraper.LocalizedTitle{Language: "zh", Title: second.Title}) {
		t.Errorf("unexpected Chinese fallback or localized title: %#v", second)
	}
	for _, track := range items[0].Tracks {
		fields := music.TrackFields{Title: track.Title, LocalizedTitles: track.LocalizedTitles}
		if _, err := (music.TrackInput{TrackFields: fields}).Normalize(); err != nil {
			t.Errorf("unusable track metadata for episode %s: %v", track.ExternalID, err)
		}
	}
}

func TestDLsiteUsesOnlyRequestedProductAndDeclaredImageHost(t *testing.T) {
	client := lookupTestClient(t, "www.dlsite.com", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/home/product/info/ajax" || r.URL.Query().Get("product_id") != "RJ00012345" {
			t.Errorf("unexpected URL %s", r.URL)
		}
		fmt.Fprint(w, `{"RJ00012345":{"site_id":"home","work_name":"Voice Work","maker_name":"Circle","work_image":"//img.dlsite.jp/modpub/voice.jpg","regist_date":"2026-02-01"},"RJ123456":{"work_name":"Ignored"}}`)
	})
	plugin := DLsite(client)
	items, err := plugin.Search(t.Context(), "artwork", scraper.Query{ExternalID: "rj00012345"})
	if err != nil || len(items) != 1 || items[0].Title != "Voice Work" || items[0].ImageURL != "https://img.dlsite.jp/modpub/voice.jpg" || items[0].Artist != "Circle" {
		t.Fatalf("unexpected work %#v %v", items, err)
	}
	if _, err := plugin.Search(t.Context(), "workDetails", scraper.Query{ExternalID: "RJ123456&other"}); err == nil {
		t.Fatal("invalid work ID accepted")
	}
}

package online

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

const qqAlbumFixture = `{"basicInfo":{"albumMid":"AlbumM","albumName":"Edition","publishDate":"2002-03-04","genreNew":"Pop","pmid":"AlbumM_2"},"company":{"name":"Label"},"singer":{"singerList":[{"name":"Artist"}]}}`
const qqSongFixture = `{"mid":"SongA","title":"A &amp; B","interval":61,"index_cd":0,"index_album":1,"singer":[{"name":"First"},{"name":"Second"}],"album":{"mid":"AlbumM","name":"Edition","pmid":"AlbumM_2"}}`

type qqTestRequest struct {
	Module, Method string
	Param          struct {
		AlbumMID          string
		SongMID           string `json:"song_mid"`
		Begin, Num, Order int
	}
}

func qqTestClient(t *testing.T, term string, rpc func(qqTestRequest) string) *http.Client {
	t.Helper()
	return &http.Client{Transport: lookupTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Scheme != "https" || r.Header.Get("Cookie") != "" {
			return nil, fmt.Errorf("unexpected request to %s", r.URL)
		}
		w := httptest.NewRecorder()
		switch r.URL.Host + r.URL.Path {
		case "c.y.qq.com/splcloud/fcgi-bin/smartbox_new.fcg":
			if term == "" || r.URL.Query().Get("key") != term || r.URL.Query().Get("format") != "json" {
				t.Fatalf("unexpected suggestions query %s", r.URL)
			}
			fmt.Fprint(w, `{"code":0,"data":{"song":{"itemlist":[{"mid":"SongA"}]},"album":{"itemlist":[{"mid":"AlbumM"},{"mid":"AlbumM"},{"mid":"../bad"}]}}}`)
		case "u.y.qq.com/cgi-bin/musicu.fcg":
			var payload struct {
				Call qqTestRequest `json:"req_0"`
			}
			if err := json.Unmarshal([]byte(r.URL.Query().Get("data")), &payload); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(w, `{"code":0,"req_0":{"code":0,"data":%s}}`, rpc(payload.Call))
		default:
			return nil, fmt.Errorf("unexpected API %s", r.URL)
		}
		return w.Result(), nil
	})}
}

func TestQQMusicAlbumSearchAndExactTrackList(t *testing.T) {
	detailRequests := 0
	client := qqTestClient(t, "Edition", func(r qqTestRequest) string {
		if r.Param.AlbumMID != "AlbumM" {
			t.Fatalf("unexpected album ID %s", r.Param.AlbumMID)
		}
		switch r.Module + "." + r.Method {
		case "music.musichallAlbum.AlbumInfoServer.GetAlbumDetail":
			detailRequests++
			return qqAlbumFixture
		case "music.musichallAlbum.AlbumSongList.GetAlbumSongList":
			if r.Param.Num <= 0 || r.Param.Order != 2 {
				t.Fatalf("unexpected album page parameters %#v", r.Param)
			}
			switch r.Param.Begin {
			case 0:
				return `{"albumMid":"AlbumM","curBegin":0,"totalNum":2,"songList":[{"songInfo":` + qqSongFixture + `}]}`
			case 1:
				return `{"albumMid":"AlbumM","curBegin":1,"totalNum":2,"songList":[{"songInfo":{"mid":"SongB","title":"B","index_cd":1,"index_album":1,"album":{"mid":"AlbumM"}}}]}`
			}
		}
		t.Fatalf("unexpected call %#v", r)
		return ""
	})
	plugin := QQMusic(client)
	items, err := plugin.Search(t.Context(), "albumDetails", scraper.Query{Title: "Edition", Artist: "Artist"})
	if err != nil || len(items) != 1 || detailRequests != 1 {
		t.Fatalf("unexpected editions %#v %v", items, err)
	}
	item := items[0]
	if item.ID != "AlbumM" || item.Title != "Edition" || item.Artist != "Artist" || item.Genre != "Pop" || item.Publisher != "Label" ||
		item.Date != "2002-03-04" || item.ImageURL != "https://y.gtimg.cn/music/photo_new/T002R300x300M000AlbumM_2.jpg" {
		t.Fatalf("unexpected album details %#v", item)
	}
	items, err = plugin.Search(t.Context(), "albumTracks", scraper.Query{ExternalID: item.ID, SourceURL: item.SourceURL})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != 2 {
		t.Fatalf("unexpected album tracks %#v %v", items, err)
	}
	a, b := items[0].Tracks[0], items[0].Tracks[1]
	if a.ExternalID != "SongA" || a.Title != "A & B" || a.Artist != "First / Second" || a.Disc != 1 || a.Number != 1 || a.Duration != 61 || b.Disc != 2 || b.Number != 1 {
		t.Fatalf("unexpected track metadata %#v", items[0].Tracks)
	}
}

func TestQQMusicTrackSuggestionsAndExactIdentity(t *testing.T) {
	client := qqTestClient(t, "A", func(r qqTestRequest) string {
		if r.Module != "music.pf_song_detail_svr" || r.Method != "get_song_detail_yqq" {
			t.Fatalf("unexpected method %#v", r)
		}
		return `{"track_info":` + qqSongFixture + `}`
	})
	plugin := QQMusic(client)
	items, err := plugin.Search(t.Context(), "trackDetails", scraper.Query{Title: "A"})
	if err != nil || len(items) != 1 || items[0].ID != "SongA" || items[0].Album != "Edition" || items[0].Duration != 61 || items[0].SourceURL != "https://y.qq.com/n/ryqq/songDetail/SongA" {
		t.Fatalf("unexpected track details %#v %v", items, err)
	}
	items, err = plugin.Search(t.Context(), "trackDetails", scraper.Query{ExternalID: "SongB"})
	if err != nil || len(items) != 0 {
		t.Fatalf("returned wrong track %#v %v", items, err)
	}
	if _, err := plugin.Search(t.Context(), "trackDetails", scraper.Query{ExternalID: "../song"}); err == nil {
		t.Fatal("accepted invalid MID")
	}
}

func TestQQMusicLyricsFromSongMID(t *testing.T) {
	for _, tc := range []struct{ body, format string }{
		{"[00:12.21]One &amp; two", "lrc"}, {"Plain lyrics", "text"}, {"", ""},
	} {
		catalog := qqTestClient(t, "A", func(r qqTestRequest) string {
			if r.Param.SongMID != "SongA" {
				t.Fatalf("unexpected song %#v", r)
			}
			return `{"track_info":` + qqSongFixture + `}`
		})
		transport := catalog.Transport
		catalog.Transport = lookupTestTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/lyric/fcgi-bin/fcg_query_lyric_new.fcg" {
				return transport.RoundTrip(r)
			}
			if r.URL.Host != "c.y.qq.com" || r.URL.Query().Get("songmid") != "SongA" || r.URL.Query().Get("nobase64") != "1" || r.Header.Get("Referer") != "https://y.qq.com/" {
				t.Fatalf("unexpected lyrics request %v", r.URL)
			}
			w := httptest.NewRecorder()
			if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "lyric": tc.body}); err != nil {
				t.Fatal(err)
			}
			return w.Result(), nil
		})
		items, err := QQMusic(catalog).Search(t.Context(), "lyrics", scraper.Query{Title: "A"})
		if err != nil {
			t.Fatal(err)
		}
		if tc.format == "" {
			if len(items) != 0 {
				t.Fatal("returned empty lyrics", items)
			}
		} else if len(items) != 1 || items[0].Format != tc.format || items[0].Lyrics != html.UnescapeString(tc.body) || items[0].ID != "SongA" {
			t.Fatal(items)
		}
	}
}

func TestQQMusicRejectsPartialOrMismatchedAlbum(t *testing.T) {
	for _, page := range []string{
		`{"albumMid":"AlbumM","curBegin":0,"totalNum":1,"songList":[]}`,
		`{"albumMid":"Other","curBegin":0,"totalNum":0,"songList":[]}`,
		`{"albumMid":"AlbumM","curBegin":1,"totalNum":1,"songList":[{"songInfo":` + qqSongFixture + `}]}`,
		`{"albumMid":"AlbumM","curBegin":0,"totalNum":2,"songList":[{"songInfo":` + qqSongFixture + `},{"songInfo":` + qqSongFixture + `}]}`,
	} {
		client := qqTestClient(t, "", func(r qqTestRequest) string {
			if r.Method == "GetAlbumDetail" {
				return qqAlbumFixture
			}
			return page
		})
		items, err := QQMusic(client).Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "AlbumM"})
		if err == nil || len(items) > 0 {
			t.Fatalf("incomplete album accepted %#v %v", items, err)
		}
	}
}

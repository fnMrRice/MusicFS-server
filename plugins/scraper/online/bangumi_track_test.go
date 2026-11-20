package online

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func TestBangumiEpisodeTitlesAndCredits(t *testing.T) {
	tests := []struct {
		name, cn, desc                             string
		title, artist, composer, zhTitle, zhArtist string
	}{
		{name: "鳥 - 幸祜 &amp; 春猿火", title: "鳥", artist: "幸祜 & 春猿火"},
		{name: "鳥 - Guiano", title: "鳥", artist: "Guiano"},
		{name: "鳥", title: "鳥"},
		{name: "鳥 - Instrumental", title: "鳥 - Instrumental"},
		{name: "ぽっぴっぽー　/　ラマーズP feat.初音ミク", title: "ぽっぴっぽー", artist: "ラマーズP feat.初音ミク"},
		{name: "永久に続く五線譜 / デッドボールP loves 初音ミク", title: "永久に続く五線譜", artist: "デッドボールP loves 初音ミク"},
		{name: "トゥッティ! (TV Size)", cn: "歌：北宇治カルテット", title: "トゥッティ! (TV Size)", artist: "北宇治カルテット"},
		{name: "Song\n作曲：First / 演唱：Singer (CV: Second)", cn: "歌名（作曲：甲、演唱：乙）",
			title: "Song", artist: "Singer (CV: Second)", composer: "First", zhTitle: "歌名", zhArtist: "乙"},
		{name: "Melody （作曲：作者）", title: "Melody", composer: "作者"},
		{name: "Melody / 作詞・作曲・編曲：作者", title: "Melody", composer: "作者"},
		{name: "Melody / 作曲／演唱：作者", title: "Melody", artist: "作者", composer: "作者"},
		{name: "Song (作曲：A) (演唱：B (CV: C))", title: "Song", artist: "B (CV: C)", composer: "A"},
		{name: "【作曲：A】【作詞：C】【演唱：B】", cn: "歌名", title: "歌名", artist: "B", composer: "A", zhTitle: "歌名"},
		{name: "Song / 山田", title: "Song", artist: "山田"},
		{name: "Song / 春猿火 / 幸祜", title: "Song", artist: "春猿火 / 幸祜"},
		{name: "Stars/Light - After / Before", cn: "星光 / 曙光", title: "Stars/Light - After / Before", zhTitle: "星光 / 曙光"},
		{name: "Rock - Side A", title: "Rock - Side A"},
		{name: "Rock", title: "Rock"},
		{name: "Rock - After the Rain", title: "Rock - After the Rain"},
		{name: "鳥 - TV Size", title: "鳥 - TV Size"},
		{name: "Plain &amp; Simple", desc: "作曲：One\n編曲：Two\nVocal: Three\nA review of the song",
			title: "Plain & Simple", composer: "One", artist: "Three"},
		{name: "No credits", desc: "感想：心に響く歌。作曲：という言葉も出た", title: "No credits"},
		{name: "花 - Alex", title: "花", artist: "Alex"},
		{name: "花 - Guiano", title: "花", artist: "Guiano"},
		{name: "花", title: "花"},
	}
	client := lookupTestClient(t, "api.bgm.tv", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/subjects/23":
			fmt.Fprint(w, `{"id":23,"type":3,"name":"Album","infobox":[{"key":"艺术家","value":"理芽、幸祜、春猿火、Guiano"},{"key":"作曲","value":[{"v":"山田"}]}]}`)
		case "/v0/episodes":
			rows := make([]bangumiEpisode, 0, len(tests))
			for i, test := range tests {
				rows = append(rows, bangumiEpisode{ID: int64(100 + i), Name: test.name, NameCN: test.cn,
					Desc: test.desc, Disc: 1, Sort: float64(i + 1), Duration: 90})
			}
			// 同名无署名章节可以在另一页，由整个专辑给歧义分隔符提供依据。
			if r.URL.Query().Get("offset") == "0" {
				rows = rows[:len(rows)-1]
			} else {
				rows = rows[len(rows)-1:]
			}
			if err := json.NewEncoder(w).Encode(map[string]any{"total": len(tests), "data": rows}); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	})
	items, err := Bangumi(client).Search(t.Context(), "albumTracks", scraper.Query{ExternalID: "23"})
	if err != nil || len(items) != 1 || len(items[0].Tracks) != len(tests) {
		t.Fatalf("unexpected album tracks: %#v %v", items, err)
	}
	checkAlias := func(names []scraper.LocalizedTitle, want string) bool {
		return want == "" && len(names) == 0 || len(names) == 1 && names[0] == (scraper.LocalizedTitle{Language: "zh", Title: want})
	}
	for i, want := range tests {
		track := items[0].Tracks[i]
		if track.Title != want.title || track.Artist != want.artist || track.Composer != want.composer ||
			!checkAlias(track.LocalizedTitles, want.zhTitle) || !checkAlias(track.LocalizedArtistNames, want.zhArtist) ||
			track.ExternalID != fmt.Sprint(100+i) || track.Disc != 1 || track.Number != i+1 || track.Duration != 90 {
			t.Errorf("episode %d (%s): unexpected track %#v", i, want.name, track)
		}
	}
}

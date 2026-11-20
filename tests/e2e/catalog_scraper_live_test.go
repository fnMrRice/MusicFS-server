package e2e_test

import (
	"os"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

// 通过 Web 使用的搜索接口验证公开目录；测试库独立运行，无需平台账号。
func TestPlatformScraperLiveAlbumHTTP(t *testing.T) {
	if os.Getenv("MUSICFS_ONLINE_LIVE") != "1" {
		t.Skip("explicit public-network validation not enabled")
	}
	cfg, base := yamlServiceConfig(t)
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	for _, tc := range []struct {
		plugin, album, id, page string
	}{
		{"musicfs.scraper.netease", "周杰伦的床边故事", "34720827", "https://music.163.com/#/album?id=34720827"},
		{"musicfs.scraper.qqmusic", "Jay", "000f01724fd7TH", "https://y.qq.com/n/ryqq/albumDetail/000f01724fd7TH"},
	} {
		t.Run(tc.plugin, func(t *testing.T) {
			search := func(field string, query scraper.Query) []onlinescrape.Candidate {
				t.Helper()
				page := albumRequest[struct{ Items []onlinescrape.Candidate }](t, c, "POST", "/scrapers/"+tc.plugin+"/search",
					map[string]any{"field": field, "query": query}, "", 200, "OnlineScraperCandidates")
				return page.Items
			}
			if items := search("albumDetails", scraper.Query{Title: tc.album}); len(items) == 0 {
				t.Fatal("album name search returned no candidates")
			}
			details := search("albumDetails", scraper.Query{SourceURL: tc.page})
			if len(details) != 1 || details[0].ID != tc.id || details[0].SourceURL == "" || details[0].ImageURL == "" {
				t.Fatal("album page lookup did not yield the requested edition and its cover")
			}
			album := search("albumTracks", scraper.Query{ExternalID: tc.id, SourceURL: details[0].SourceURL})
			if len(album) != 1 || album[0].ID != tc.id || len(album[0].Tracks) != 10 {
				t.Fatal("edition lookup did not yield the complete ten-track album")
			}
			trackID := album[0].Tracks[0].ExternalID
			if tracks := search("trackDetails", scraper.Query{ExternalID: trackID}); len(tracks) != 1 || tracks[0].ID != trackID {
				t.Fatal("track lookup returned a different recording")
			}
			if tc.plugin == "musicfs.scraper.netease" {
				if lyrics := search("lyrics", scraper.Query{ExternalID: trackID}); len(lyrics) != 1 || lyrics[0].ID != trackID || lyrics[0].Lyrics == "" {
					t.Fatal("lyrics lookup did not return text for the requested track")
				}
			}
			target := albumRequest[music.Album](t, c, "POST", "/library/albums", map[string]string{"name": tc.album},
				tc.plugin+"-create", 201, "MusicAlbum")
			albumRequest[struct{ Revision int }](t, c, "PUT", "/library/albums/"+target.ID+"/scraped-fields",
				map[string]any{"expectedRevision": target.Revision, "pluginId": tc.plugin,
					"candidates": map[string]string{"albumDetails": details[0].Token, "albumTracks": album[0].Token}},
				tc.plugin+"-select", 200, "ScrapedMusicAlbumRevision")
			saved := albumRequest[struct{ Items []music.Track }](t, c, "GET", "/library/tracks?albumId="+target.ID, nil, "", 200, "MusicTrackPage")
			if len(saved.Items) != len(album[0].Tracks) {
				t.Fatal("the selected edition was not saved with all its tracks")
			}
		})
	}
}

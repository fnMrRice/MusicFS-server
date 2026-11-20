package online

import (
	"cmp"
	"context"
	"errors"
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

var qqPictureMID = regexp.MustCompile(`^[A-Za-z0-9]{1,64}(_[0-9]{1,12})?$`)

func qqCover(pmid string) string {
	if !qqPictureMID.MatchString(pmid) {
		return ""
	}
	return "https://y.gtimg.cn/music/photo_new/T002R300x300M000" + pmid + ".jpg"
}

func qqAlbum(ctx context.Context, client *http.Client, mid, field string) (scraper.Item, error) {
	type details struct {
		BasicInfo struct{ AlbumMID, AlbumName, PublishDate, Desc, GenreNew, Genre, PMID string }
		Company   struct{ Name string }
		Singer    struct{ SingerList []qqSinger }
	}
	data, err := qqCatalogRPC[details](ctx, client, "music.musichallAlbum.AlbumInfoServer", "GetAlbumDetail", map[string]any{"albumMid": mid})
	if err != nil {
		return scraper.Item{}, err
	}
	info := data.BasicInfo
	if info.AlbumMID != mid {
		return scraper.Item{}, errCatalogMissing
	}
	name := strings.TrimSpace(html.UnescapeString(info.AlbumName))
	if name == "" {
		return scraper.Item{}, errors.New("QQ Music album title is missing")
	}
	item := scraper.Item{ID: mid, Title: name, Album: name, Artist: qqCredit(data.Singer.SingerList),
		Date: info.PublishDate, Genre: cmp.Or(info.GenreNew, info.Genre), Publisher: html.UnescapeString(data.Company.Name),
		Description: html.UnescapeString(info.Desc), ImageURL: qqCover(info.PMID), SourceURL: "https://y.qq.com/n/ryqq/albumDetail/" + mid}
	if field == "albumTracks" {
		item.Tracks, err = qqAlbumTracks(ctx, client, mid)
	}
	return item, err
}

func qqAlbumTracks(ctx context.Context, client *http.Client, mid string) ([]scraper.Track, error) {
	type trackPage struct {
		AlbumMID           string
		TotalNum, CurBegin *int
		SongList           []struct{ SongInfo qqSongInfo }
	}
	const pageSize, maxTracks = 100, 2000
	var tracks []scraper.Track
	expected := -1
	type slot struct {
		mid          string
		disc, number int
	}
	seen := map[slot]bool{}
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber >= 100 {
			return nil, errors.New("too many QQ Music album pages")
		}
		offset := len(tracks)
		params := map[string]any{"albumMid": mid, "begin": offset, "num": pageSize, "order": 2}
		page, err := qqCatalogRPC[trackPage](ctx, client, "music.musichallAlbum.AlbumSongList", "GetAlbumSongList", params)
		if err != nil {
			return nil, err
		}
		if page.AlbumMID != mid || page.CurBegin == nil || *page.CurBegin != offset || page.TotalNum == nil ||
			*page.TotalNum < 0 || *page.TotalNum > maxTracks || expected >= 0 && expected != *page.TotalNum {
			return nil, errors.New("invalid QQ Music album page")
		}
		expected = *page.TotalNum
		if len(page.SongList) == 0 && expected > offset || len(page.SongList) > pageSize || offset+len(page.SongList) > expected {
			return nil, errors.New("incomplete QQ Music album track list")
		}
		for _, row := range page.SongList {
			track, err := row.SongInfo.track()
			if err != nil {
				return nil, err
			}
			key := slot{track.ExternalID, track.Disc, track.Number}
			if seen[key] || row.SongInfo.Album.MID != "" && row.SongInfo.Album.MID != mid {
				return nil, errors.New("unexpected entry in QQ Music album track list")
			}
			seen[key] = true
			tracks = append(tracks, track)
		}
		if len(tracks) == expected {
			break
		}
	}
	slices.SortStableFunc(tracks, func(a, b scraper.Track) int {
		return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Number, b.Number))
	})
	return tracks, nil
}

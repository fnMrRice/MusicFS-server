package online

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type storeNameKey struct {
	id, collection string
}

type storeNameTarget struct {
	primary    string
	names      *[]scraper.LocalizedTitle
	albumTrack *scraper.Track
}

// Apple 的地区商店可为同一编号返回不同名称。按整批编号查其他商店，
// 曲目同时核对所属专辑；客串或联合署名不作为单个艺人的别名。
func enrichStoreNames(ctx context.Context, client *http.Client, country, field string, entries []itunesEntry, items []scraper.Item) {
	targets := make(map[storeNameKey]storeNameTarget)
	var ids []string
	params := url.Values{}
	switch field {
	case "albumTracks":
		for i := range items {
			item := &items[i]
			for j := range item.Tracks {
				track := &item.Tracks[j]
				if track.ExternalID != "" {
					targets[storeNameKey{track.ExternalID, item.ID}] = storeNameTarget{primary: track.Title, names: &track.LocalizedTitles, albumTrack: track}
				}
			}
			ids = append(ids, item.ID)
		}
		params.Set("entity", "song")
		params.Set("limit", "200")
	case "trackDetails":
		albums := make(map[string]string)
		for _, row := range entries {
			if row.WrapperType == "track" && row.Kind == "song" && row.TrackID > 0 && row.CollectionID > 0 {
				albums[numericTrackID(row.TrackID)] = numericTrackID(row.CollectionID)
			}
		}
		for i := range items {
			item := &items[i]
			if albumID := albums[item.ID]; albumID != "" {
				targets[storeNameKey{item.ID, albumID}] = storeNameTarget{primary: item.Title, names: &item.LocalizedTitles}
				ids = append(ids, item.ID)
			}
		}
	case "artistDetails":
		for i := range items {
			item := &items[i]
			targets[storeNameKey{id: item.ID}] = storeNameTarget{primary: item.Title, names: &item.LocalizedArtistNames}
			ids = append(ids, item.ID)
		}
	}
	if len(targets) == 0 {
		return
	}
	slices.Sort(ids)
	params.Set("id", strings.Join(slices.Compact(ids), ","))
	// 附加语言查询共用限速和总预算，商店缺少授权或暂时不可用时仍保留主结果。
	optional, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	for _, storefront := range []string{"JP", "US", "TW"} {
		if storefront == country || optional.Err() != nil {
			continue
		}
		params.Set("country", storefront)
		rows, err := itunesQuery(optional, client, "lookup", params)
		if err != nil {
			continue
		}
		for _, row := range rows {
			var key storeNameKey
			var name string
			switch {
			case field == "artistDetails" && row.WrapperType == "artist" && row.ArtistID > 0:
				key.id, name = numericTrackID(row.ArtistID), row.ArtistName
			case field != "artistDetails" && row.WrapperType == "track" && row.Kind == "song":
				key = storeNameKey{numericTrackID(row.TrackID), numericTrackID(row.CollectionID)}
				name = row.TrackName
			default:
				continue
			}
			if target, ok := targets[key]; ok {
				// 商店地区并不声明名称的语言，例如日本商店也会返回英文曲名。
				*target.names = addPlatformName(*target.names, target.primary, "und", name)
				if track := target.albumTrack; track != nil {
					track.LocalizedArtistNames = addPlatformName(track.LocalizedArtistNames, track.Artist, "und", row.ArtistName)
				}
			}
		}
	}
}

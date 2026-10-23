package bilibili

import (
	"context"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// 集合搜索只读取资料，不展开分 P 或抓取整张歌单；条目数在完整观察后确定。
func (c *Client) playlistMetadata(ctx context.Context, r reference) (online.Playlist, error) {
	switch r.kind {
	case "favorite":
		var page favoritePage
		if err := c.get(ctx, apiRoot+"/x/v3/fav/resource/list", url.Values{"media_id": {r.id}, "pn": {"1"}, "ps": {"1"}}, &page); err != nil {
			return online.Playlist{}, err
		}
		if strconv.FormatInt(page.Info.ID, 10) != r.id {
			return online.Playlist{}, online.UpstreamInvalid()
		}
		if page.Info.Attr != nil && *page.Info.Attr&1 != 0 {
			return online.Playlist{}, fault.Fail("ACCESS_DENIED", "Bilibili favorites are private")
		}
		return playlist(r.canonical(), page.Info.Title, page.Info.Upper.Name)
	case "audio-menu":
		var info struct {
			ID    int64  `json:"menuId"`
			Title string `json:"title"`
			Uname string `json:"uname"`
		}
		if err := c.get(ctx, audioRoot+"/menu/info", url.Values{"sid": {r.id}}, &info); err != nil {
			return online.Playlist{}, err
		}
		if strconv.FormatInt(info.ID, 10) != r.id {
			return online.Playlist{}, online.UpstreamInvalid()
		}
		return playlist(r.canonical(), info.Title, info.Uname)
	default:
		return online.Playlist{}, fault.Fail("INVALID_ARGUMENT", "A Bilibili favorites or audio menu reference is required")
	}
}

package api_test

import (
	"context"
	"strconv"
	"time"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// 模拟仍声明单独每日曲目能力的第三方插件，覆盖通用接口的数量限制与顺序。
type dailyTracksFixture struct{ *onlineplugins.Client }

func (p dailyTracksFixture) Descriptor() pluginproto.OnlineDescriptor {
	d := onlineplugins.Describe(online.Netease)
	for i := range d.Capabilities {
		if d.Capabilities[i].Name == "recommendations.tracks" {
			d.Capabilities[i].Supported = true
			d.Capabilities[i].Reason = "Accounts can read their daily tracks."
			return d
		}
	}
	d.Capabilities = append(d.Capabilities, online.Capability{Name: "recommendations.tracks", Supported: true, Reason: "Accounts can read their daily tracks."})
	return d
}

func (dailyTracksFixture) DailySongs(_ context.Context, limit int) (online.DailySongsPage, error) {
	page := online.DailySongsPage{ObservedAt: time.Now().UTC(), Tracks: make([]online.Track, limit)}
	for i := range page.Tracks {
		id := strconv.Itoa(i + 1)
		track := online.Track{ExternalID: id, Title: "Daily track", Artists: []string{}, Album: "Album",
			SourceURL: "https://music.163.com/song?id=" + id, PlaybackAvailability: "unknown"}
		track.MetadataDigest = online.TrackDigest(track)
		page.Tracks[i] = track
	}
	return page, nil
}

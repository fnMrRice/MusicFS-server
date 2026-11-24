package onlinecache

import "context"

// TrackVersionReader 按 MusicFS 用户、来源实例和当前页的外部曲目编号读取下载记录。
type TrackVersionReader interface {
	OnlineTrackVersions(context.Context, string, string, []string) (map[string][]Version, error)
}

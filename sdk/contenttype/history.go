package contenttype

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// HistoryEntry 是宿主确认已播放的音轨及其所属作品；时间取自首次确认的回执。
type HistoryEntry struct {
	WorkID        string    `json:"workId"`
	WorkTitle     string    `json:"workTitle"`
	SourceTrackID string    `json:"sourceTrackId"`
	TrackTitle    string    `json:"trackTitle"`
	RecordedAt    time.Time `json:"recordedAt"`
}

// History 接管本类型的记录、排序和保留规则。重复投递同一回执应保持幂等。
type History interface {
	Record(context.Context, string, HistoryEntry) error
	List(context.Context, string) ([]HistoryEntry, error)
}

// HistoryState 由宿主绑定到当前内容插件的私有命名空间。
type HistoryState interface {
	Get(context.Context, string) (pluginproto.StateValue, error)
	CompareAndSet(context.Context, pluginproto.StateRequest) (pluginproto.StateValue, error)
}

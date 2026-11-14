package personal

import (
	"context"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// HistoryTarget 随宿主的幂等回执保存，重试时使用首次确认的作品归属。
type HistoryTarget struct {
	TypeID        string `json:"typeId"`
	WorkID        string `json:"workId"`
	WorkTitle     string `json:"workTitle"`
	SourceTrackID string `json:"sourceTrackId"`
	TrackTitle    string `json:"trackTitle"`
}

type HistoryDelivery struct {
	EventID string                   `json:"eventId"`
	Owner   string                   `json:"owner"`
	Entry   contenttype.HistoryEntry `json:"entry"`
}

// DeliverHistory 可在启动、读取或收到播放回执时重试。插件按原播放时间处理重复投递。
func (s *Service) DeliverHistory(ctx context.Context, owner, id string) error {
	provider := s.histories[id]
	if provider == nil {
		return domain.Fail("NOT_FOUND", "Content history is unavailable")
	}
	for {
		events, err := s.Repository.PendingContentHistory(ctx, owner, id)
		if err != nil || len(events) == 0 {
			return err
		}
		for _, event := range events {
			if err := provider.Record(ctx, event.Owner, event.Entry); err != nil {
				return err
			}
			if err := s.Repository.ConfirmContentHistory(ctx, event.Owner, id, event.EventID); err != nil {
				return err
			}
		}
	}
}

// RegisterHistory 在开始服务请求前按插件声明顺序注册。
func (s *Service) RegisterHistory(id string, history contenttype.History) {
	if s.histories == nil {
		s.histories = make(map[string]contenttype.History)
	}
	s.histories[id] = history
	s.separateTypes = append(s.separateTypes, id)
}

func (s *Service) ContentHistory(ctx context.Context, owner, id string) ([]contenttype.HistoryEntry, error) {
	if err := s.DeliverHistory(ctx, owner, id); err != nil {
		return nil, err
	}
	return s.histories[id].List(ctx, owner)
}

func (s *Service) RecordPlaybackStarted(ctx context.Context, who auth.Session, input PlaybackStarted) (Entry, error) {
	input.SeparateTypes = slices.Clone(s.separateTypes)
	entry, err := s.Repository.RecordPlaybackStarted(ctx, who, input)
	if err != nil || entry.HistoryTarget == nil {
		return entry, err
	}
	err = s.DeliverHistory(ctx, who.User.ID, entry.HistoryTarget.TypeID)
	entry.HistoryTarget = nil
	return entry, err
}

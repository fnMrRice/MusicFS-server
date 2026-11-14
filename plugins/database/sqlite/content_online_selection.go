package sqlite

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

// 对在线选用按用户提交的完整输入计算幂等摘要。媒体对象编号由宿主生成，
// 不属于用户输入，只有实际选用时才需要它。
func (s *Store) beginSelection(ctx context.Context, owner string, request mediastore.SelectRequest, key string) (*contentMutation, bool, error) {
	operation := "select"
	if request.Online != nil {
		candidate := request.Online
		if request.Automatic || candidate.PluginID == "" || len(candidate.PluginID) > 128 || candidate.Token == "" || len(candidate.Token) > 128 || (candidate.Field != "lyrics" && candidate.Field != "artwork") {
			return nil, false, domain.Fail("INVALID_ARGUMENT", "Invalid online selection")
		}
		operation = "select-online"
		request.ObjectID = ""
	}
	return s.beginContentMutation(ctx, owner, request.Target.SourceTrackID, operation, key, request.ExpectedRevision, struct {
		Request   mediastore.SelectRequest
		Automatic bool
	}{request, request.Automatic})
}

// CheckOnlineSelection 在访问远端前检查幂等记录、目标版本和当前修订。
// 首次调用只读；后续选用仍在最终事务内重新核对。
func (s *Store) CheckOnlineSelection(ctx context.Context, owner string, request mediastore.SelectRequest, key string) (mediastore.Selection, bool, error) {
	if request.Online == nil {
		return mediastore.Selection{}, false, domain.Fail("INVALID_ARGUMENT", "Online selection required")
	}
	m, replay, err := s.beginSelection(ctx, owner, request, key)
	if err != nil {
		return mediastore.Selection{}, false, err
	}
	defer m.tx.Rollback()
	if !replay {
		if err := contentCurrentTarget(ctx, m.tx, owner, request.Target); err != nil {
			return mediastore.Selection{}, false, err
		}
	}
	return m.result, replay, nil
}

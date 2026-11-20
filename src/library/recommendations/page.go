package recommendations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// Page 按服务器日历日维护整页快照，手动刷新或跨日才重新读取上游。
func (s *Service) Page(ctx context.Context, owner string, request Request) (Response, error) {
	day := s.config.Now().Format(time.DateOnly)
	if request.History < 0 || request.History > MaxHistory || (request.History > 0 && request.Refresh) {
		return Response{}, domain.Fail("INVALID_ARGUMENT", "Invalid recommendation history selection")
	}
	runtimes, err := s.selectRuntimes(ctx, owner, request)
	if err != nil {
		return Response{}, err
	}
	if len(runtimes) == 0 && request.History == 0 {
		return Response{Sources: []Source{}}, nil
	}
	if s.config.Repository == nil {
		return Response{}, domain.Fail("TEMPORARY_FAILURE", "Recommendation history storage is unavailable")
	}
	scope, err := s.pageScope(ctx, owner, request.Limit, runtimes)
	if err != nil {
		return Response{}, err
	}
	var result Response
	if request.History > 0 {
		var saved SavedPage
		saved, err = s.config.Repository.ReadRecommendationPage(ctx, owner, scope.key, request.History)
		if err == nil && saved.Sources == nil {
			return Response{}, domain.Fail("NOT_FOUND", "Recommendation history was not found")
		}
		result = Response{Sources: saved.Sources, HistoryDepth: saved.HistoryDepth}
	} else {
		result, err = s.currentPage(ctx, owner, scope, day, runtimes, request)
	}
	if err == nil {
		err = s.checkPageScope(ctx, owner, request.Limit, runtimes, scope)
	}
	if err == nil {
		err = scope.checkSources(result.Sources)
	}
	if err != nil {
		return Response{}, err
	}
	return result, nil
}

type sourceScope struct {
	Instance, Platform, Config, Account, Login, Failure string
}

type pageScope struct {
	key, session string
	claims       []sourceScope
}

func (s *Service) pageScope(ctx context.Context, owner string, limit int, runtimes []Runtime) (pageScope, error) {
	parts := make([]sourceScope, len(runtimes))
	revisions := make([]string, len(runtimes))
	for i, r := range runtimes {
		part := sourceScope{Instance: r.InstanceID, Platform: r.Platform, Config: r.ConfigRevision}
		err := r.Gate.Check(ctx)
		if err == nil {
			switch p := r.Provider.(type) {
			case nil:
				err = domain.Fail("SOURCE_OFFLINE", "Recommendation source is offline")
			case interface {
				RecommendationAccountScope(context.Context) (string, string, string, error)
			}:
				part.Account, revisions[i], part.Login, err = p.RecommendationAccountScope(ctx)
			case interface {
				AccountScope(context.Context) (string, string, error)
			}:
				part.Account, part.Login, err = p.AccountScope(ctx)
			case interface{ CheckAvailability(context.Context) error }:
				err = p.CheckAvailability(ctx)
			}
		}
		if err != nil {
			failure, _ := failureFor(err, s.config.FailureCooldown)
			part.Account, part.Login, part.Failure = "", "", failure.Code
		}
		parts[i] = part
	}
	if err := ctx.Err(); err != nil {
		return pageScope{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return pageScope{}, domain.Fail("SOURCE_OFFLINE", "Recommendation service is draining")
	}
	for _, r := range runtimes {
		if s.runtimes[runtimeKey{owner, r.InstanceID}].generation != r.generation {
			return pageScope{}, domain.Fail("RESOURCE_CHANGED", "Recommendation instance changed")
		}
	}
	encoded, err := json.Marshal([]any{owner, limit, parts})
	if err != nil {
		return pageScope{}, err
	}
	digest := sha256.Sum256(encoded)
	sessions, err := json.Marshal(revisions)
	if err != nil {
		return pageScope{}, err
	}
	return pageScope{key: hex.EncodeToString(digest[:]), session: string(sessions), claims: parts}, nil
}

func (s *Service) checkPageScope(ctx context.Context, owner string, limit int, runtimes []Runtime, expected pageScope) error {
	current, err := s.pageScope(ctx, owner, limit, runtimes)
	if err != nil {
		return err
	}
	if current.key != expected.key || current.session != expected.session {
		return domain.Fail("RESOURCE_CHANGED", "Recommendation account or availability changed")
	}
	return nil
}

func (scope pageScope) checkSources(sources []Source) error {
	if len(sources) != len(scope.claims) {
		return domain.Fail("RESOURCE_CHANGED", "Recommendation sources changed")
	}
	for i, src := range sources {
		claim := scope.claims[i]
		hasData := src.Error == nil || src.LastSuccessAt != nil || len(src.Items) > 0
		if src.InstanceID != claim.Instance || src.Platform != claim.Platform ||
			(hasData && (claim.Failure != "" || src.AccountID != claim.Account)) ||
			(src.AccountID != "" && src.AccountID != claim.Account) {
			return domain.Fail("RESOURCE_CHANGED", "Recommendation account or availability changed")
		}
	}
	return nil
}

func (s *Service) readOrRefresh(ctx context.Context, owner string, scope pageScope, day string, runtimes []Runtime, request Request) (Response, bool, error) {
	saved, err := s.config.Repository.ReadRecommendationPage(ctx, owner, scope.key, 0)
	if err != nil {
		return Response{}, false, err
	}
	if !request.Refresh && saved.Day == day && saved.Sources != nil {
		return Response{Sources: saved.Sources, HistoryDepth: saved.HistoryDepth}, false, nil
	}
	// 跨日请求到达存储的先后顺序可能反转，较早打开的请求沿用已发布的新日页面。
	if saved.Sources != nil && day != saved.Day && s.config.Now().Format(time.DateOnly) == saved.Day {
		return Response{Sources: saved.Sources, HistoryDepth: saved.HistoryDepth}, true, nil
	}
	// 昨天临近午夜取得的来源短缓存不能代替新一天的首轮推荐。
	request.day = day
	result, err := s.listSources(ctx, runtimes, request)
	if err != nil {
		return Response{}, true, err
	}
	if err = s.checkPageScope(ctx, owner, request.Limit, runtimes, scope); err != nil {
		return Response{}, true, err
	}
	successful := false
	for i := range result.Sources {
		item := &result.Sources[i]
		if item.Error == nil {
			successful = true
		} else if (item.Error.Code == "TEMPORARY_FAILURE" || item.Error.Code == "RATE_LIMITED") && item.LastSuccessAt == nil && i < len(saved.Sources) {
			// 服务重启后，短缓存可能已清空；当日快照仍可保留上次成功的同账号内容。
			last := saved.Sources[i]
			if last.InstanceID == item.InstanceID && last.AccountID == item.AccountID && last.LastSuccessAt != nil {
				last.Stale, last.Error = true, item.Error
				*item = last
			}
		}
	}
	if err := scope.checkSources(result.Sources); err != nil {
		return Response{}, true, err
	}
	result.HistoryDepth, err = s.config.Repository.SaveRecommendationPage(ctx, owner, scope.key, day, result.Sources, successful)
	return result, true, err
}

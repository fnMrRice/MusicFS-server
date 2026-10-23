package library

import (
	"context"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// RootStatistic 按已发布的可用来源文件统计，容量保留原始文件字节数。
type RootStatistic struct {
	RootID     string `json:"rootId"`
	TrackCount int64  `json:"trackCount"`
	SizeBytes  int64  `json:"sizeBytes"`
}

type RootStatistics struct {
	Roots []RootStatistic `json:"roots"`
}

// RootStatistics 只读取已发布曲库，来源离线时仍可查询，不遍历磁盘。
func (s *Service) RootStatistics(ctx context.Context, owner, provider string) (RootStatistics, error) {
	if err := ctx.Err(); err != nil {
		return RootStatistics{}, err
	}
	runtime, ok := s.Runtime(provider)
	if !ok || runtime.Owner != owner {
		return RootStatistics{}, domain.Fail("NOT_FOUND", "Provider not found")
	}
	result, err := s.repo.RootStatistics(ctx, owner, provider)
	if err != nil {
		return RootStatistics{}, err
	}
	if result.Roots == nil {
		result.Roots = []RootStatistic{}
	}
	present := make(map[string]bool, len(result.Roots))
	for _, root := range result.Roots {
		present[root.RootID] = true
	}
	// 尚未扫描或已清空的配置根仍展示零值，不推测其磁盘内容。
	for root := range runtime.RootNames {
		if !present[root] {
			result.Roots = append(result.Roots, RootStatistic{RootID: root})
		}
	}
	sort.Slice(result.Roots, func(i, j int) bool { return result.Roots[i].RootID < result.Roots[j].RootID })
	return result, nil
}

// Package tasks 提供已接入后台操作的统一查询与取消视图。
package tasks

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Root struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Progress struct {
	Total             int `json:"total"`
	Completed         int `json:"completed"`
	Observed          int `json:"observed"`
	MetadataTotal     int `json:"metadataTotal"`
	MetadataCompleted int `json:"metadataCompleted"`
}

type Result struct {
	MergedGroups  int `json:"mergedGroups"`
	MergedSources int `json:"mergedSources"`
	SkippedGroups int `json:"skippedGroups"`
	Updated       int `json:"updated"`
	Retained      int `json:"retained"`
	NotFound      int `json:"notFound"`
	Failed        int `json:"failed"`
}

type Task struct {
	ID                 string        `json:"id"`
	Kind               string        `json:"kind"`
	OwnerID            string        `json:"ownerId"`
	OwnerName          string        `json:"ownerName"`
	ProviderInstanceID string        `json:"providerInstanceId"`
	ProviderName       string        `json:"providerName"`
	RootIDs            []string      `json:"rootIds"`
	Roots              []Root        `json:"roots"`
	Status             string        `json:"status"`
	Phase              string        `json:"phase"`
	CancelRequested    bool          `json:"cancelRequested"`
	Cancellable        bool          `json:"cancellable"`
	Progress           Progress      `json:"progress"`
	Result             Result        `json:"result"`
	CreatedAt          time.Time     `json:"createdAt"`
	UpdatedAt          time.Time     `json:"updatedAt"`
	Error              *domain.Error `json:"error"`
}

type Page struct {
	Items       []Task  `json:"items"`
	NextCursor  *string `json:"nextCursor"`
	ActiveCount int     `json:"activeCount"`
}

// Repository 的空 owner 仅供已获管理员授权的全量查询使用。
type Repository interface {
	OperationRepository
	ListRefreshTasks(context.Context, string, string, int) (Page, error)
	GetRefreshTask(context.Context, string, string) (Task, error)
	CancelRefreshTask(context.Context, string, string) (Task, error)
	RefreshTaskMetadataJobs(context.Context, string, string) ([]string, error)
	CompleteRefreshTaskCancellation(context.Context, string, string) error
}

// Package works 管理不依赖文件路径和插件运行状态的作品身份及基础资料。
package works

import (
	"context"
	"time"
)

const (
	MaxPageSize          = 100
	MaxExternalIDs       = 32
	MaxCredits           = 128
	MaxTags              = 64
	MaxTitleLength       = 256
	MaxDescriptionLength = 16384
)

type ExternalID struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type Credit struct {
	Role string `json:"role"`
	Name string `json:"name"`
}

type Work struct {
	ID              string       `json:"workId"`
	Owner           string       `json:"-"`
	ContentType     string       `json:"contentType"`
	ShowInTrackList bool         `json:"showInTrackList"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	ExternalIDs     []ExternalID `json:"externalIds"`
	Credits         []Credit     `json:"credits"`
	Tags            []string     `json:"tags"`
	Revision        int64        `json:"revision"`
	CreatedAt       time.Time    `json:"createdAt"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type CreateRequest struct {
	ContentType     string       `json:"contentType"`
	ShowInTrackList bool         `json:"showInTrackList,omitempty"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	ExternalIDs     []ExternalID `json:"externalIds"`
	Credits         []Credit     `json:"credits"`
	Tags            []string     `json:"tags"`
}

// UpdateRequest 完整替换可编辑资料；作品类型在创建后保持不变。
type UpdateRequest struct {
	ExpectedRevision int64        `json:"expectedRevision"`
	ShowInTrackList  bool         `json:"showInTrackList,omitempty"`
	Title            string       `json:"title"`
	Description      string       `json:"description"`
	ExternalIDs      []ExternalID `json:"externalIds"`
	Credits          []Credit     `json:"credits"`
	Tags             []string     `json:"tags"`
}

type Query struct {
	ContentType string `json:"contentType"`
	PackageKind string `json:"packageKind"`
	Q           string `json:"q"`
	Tag         string `json:"tag"`
	Credit      string `json:"credit"`
	After       string `json:"after"`
	Limit       int    `json:"limit"`
}

type Repository interface {
	WorkScrapedFields(context.Context, string, string) ([]ScrapedField, error)
	SelectWorkScrapedField(context.Context, string, string, int64, ScrapedField, string) (int64, error)
	CreateWork(context.Context, string, CreateRequest, string) (Work, error)
	UpdateWork(context.Context, string, string, UpdateRequest, string) (Work, error)
	Work(context.Context, string, string) (Work, error)
	Works(context.Context, string, Query) ([]Work, error)
}

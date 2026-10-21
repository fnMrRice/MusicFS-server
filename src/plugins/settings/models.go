// Package settings 管理插件声明、宿主校验和每用户持久设置。
package settings

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"time"
)

type Values map[string]any
type Record struct {
	Revision int64  `json:"revision"`
	Values   Values `json:"values"`
}
type Job struct {
	ID         *string    `json:"jobId"`
	Status     string     `json:"status"`
	Processed  int        `json:"processed"`
	Total      int        `json:"total"`
	Candidates int        `json:"candidates"`
	ErrorCode  *string    `json:"errorCode"`
	UpdatedAt  *time.Time `json:"updatedAt"`
}
type Page struct {
	PluginID string                `json:"pluginId"`
	Schema   pluginsettings.Schema `json:"schema"`
	Record
	Job Job `json:"job"`
}
type Request struct {
	ExpectedRevision *int64 `json:"expectedRevision"`
	SchemaRevision   string `json:"schemaRevision"`
	Values           Values `json:"values"`
}
type Repository interface {
	GetPluginSettings(context.Context, string, string) (Record, error)
	PutPluginSettings(context.Context, string, string, int64, Values) (Record, error)
}
type Provider struct {
	ID     string
	Schema pluginsettings.Schema
	Job    func(context.Context, string) (Job, error)
	Action func(context.Context, string, string, string) (Job, error)
}

// Package downloadjobs 管理可离开页面或重启后继续的按来源下载任务。
package downloadjobs

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type Source interface {
	DownloadBinding(context.Context, string, string) (onlinehost.DownloadBinding, error)
	DownloadQualities(context.Context, string, string) (pluginproto.QualityOptions, error)
	ResolveDownload(context.Context, string, string, string, onlinehost.DownloadBinding) (library.Track, error)
}

type Cache interface {
	DownloadOnlineTrack(context.Context, string, string, string) (onlinecache.Version, error)
}

type Request struct {
	ExternalIDs     []string `json:"externalIds"`
	Quality         string   `json:"quality"`
	AccountID       string   `json:"accountId"`
	AccountRevision string   `json:"accountRevision,omitempty"`
}
type Failure struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter *int   `json:"retryAfter,omitempty"`
}
type Item struct {
	ExternalID string               `json:"externalId"`
	State      string               `json:"state"`
	Version    *onlinecache.Version `json:"version,omitempty"`
	Error      *Failure             `json:"error,omitempty"`
}
type Summary struct {
	ID        string `json:"jobId"`
	State     string `json:"state"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}
type Job struct {
	Summary
	Items []Item `json:"items"`
}

type operationKey struct{ Owner, Instance, Key string }
type receipt struct {
	Operation   operationKey
	Digest      string
	Quality     string
	ExternalIDs []string
	Binding     onlinehost.DownloadBinding
	Created     time.Time
}

type entry struct {
	Job
	receipt
	updated time.Time
	resumed bool
}

func (e *entry) accepts(current onlinehost.DownloadBinding) bool {
	// 只在重启后首次恢复时接受同一份已验证登录的新代次。重新登录会产生新的绑定。
	if e.resumed {
		e.resumed = false
		if current.LoginBinding != "" && current.AccountID == e.Binding.AccountID && current.ConfigRevision == e.Binding.ConfigRevision && current.LoginBinding == e.Binding.LoginBinding {
			e.Binding = current
		}
	}
	return e.Binding == current
}

func newEntry(id string, r receipt) *entry {
	job := Job{Summary: Summary{ID: id, State: "queued", Total: len(r.ExternalIDs)}, Items: make([]Item, len(r.ExternalIDs))}
	for i, external := range r.ExternalIDs {
		job.Items[i] = Item{ExternalID: external, State: "queued"}
	}
	return &entry{Job: job, receipt: r, updated: r.Created}
}
func (e *entry) snapshot() Job {
	job := e.Job
	job.Items = slices.Clone(job.Items)
	return job
}
func (e *entry) next() int { return e.Completed + e.Failed }
func (e *entry) apply(items []Item, at time.Time) {
	for _, item := range items {
		e.Items[e.next()] = item
		if item.Error == nil {
			e.Completed++
		} else {
			e.Failed++
		}
	}
	e.updated = at
	if e.next() == e.Total {
		switch {
		case e.Failed == 0:
			e.State = "succeeded"
		case e.Completed == 0:
			e.State = "failed"
		default:
			e.State = "partial"
		}
	}
}

func normalize(request Request) (Request, error) {
	if len(request.ExternalIDs) < 1 || len(request.ExternalIDs) > online.MaxPlaylistEntries || len(request.Quality) > 128 || len(request.AccountID) > 128 || len(request.AccountRevision) > 128 {
		return Request{}, domain.Fail("INVALID_ARGUMENT", "Invalid online download request")
	}
	ids := make([]string, 0, len(request.ExternalIDs))
	seen := make(map[string]bool, len(request.ExternalIDs))
	for _, id := range request.ExternalIDs {
		if !online.ValidExternalID(id) {
			return Request{}, domain.Fail("INVALID_ARGUMENT", "Invalid external track identity")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	request.ExternalIDs = ids
	return request, nil
}

func failure(err error) *Failure {
	var known *domain.Error
	if errors.As(err, &known) {
		return &Failure{Code: known.Code, Message: known.Message, RetryAfter: known.RetryAfter}
	}
	result := &Failure{Code: pluginproto.ErrorCode(err), Message: "Online download could not be completed"}
	var rpc *pluginproto.RPCError
	if errors.As(err, &rpc) && rpc.Data.RetryAfterMs != nil && *rpc.Data.RetryAfterMs > 0 {
		seconds := int(1 + (*rpc.Data.RetryAfterMs-1)/1000)
		result.RetryAfter = &seconds
	}
	return result
}

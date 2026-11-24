package onlinecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type Version struct {
	ID           string    `json:"versionId"`
	Quality      string    `json:"quality"`
	MIME         string    `json:"mime"`
	Codec        string    `json:"codec"`
	Container    string    `json:"container"`
	SizeBytes    int64     `json:"sizeBytes"`
	BitrateBps   *int64    `json:"bitrateBps,omitempty"`
	DownloadedAt time.Time `json:"downloadedAt"`
}

type Scope struct {
	Owner, Plugin, Instance, Source, Account, Revision string
}

type Record struct {
	Version
	Key, Owner, Plugin, Source string
}

// DownloadTarget 在存储协议中保留完整归属，避免三个字符串参数错位。
type DownloadTarget struct {
	Owner, SourceID, VersionID string
}

func (r Record) target() DownloadTarget {
	return DownloadTarget{Owner: r.Owner, SourceID: r.Source, VersionID: r.ID}
}

type Repository interface {
	settings.Repository
	ListOnlineDownloads(context.Context) ([]Record, error)
	SaveOnlineDownload(context.Context, Record) error
	ForgetOnlineDownload(context.Context, DownloadTarget) error
}

// 表示身份来自本次平台授权结果。不会把签名 URL 或凭证写入索引。
func Key(scope Scope, audio pluginproto.OnlineAudio) string {
	// 登录代次负责撤销会话，持久副本按已验证的平台账号隔离；同账号重登并重新授权后仍可复用。
	raw, _ := json.Marshal([]any{scope.Owner, scope.Plugin, scope.Instance, scope.Source, scope.Account, audio.ExternalID, audio.MIME, audio.Codec, audio.Container, audio.Quality, audio.Version, audio.BitrateBps, audio.DurationMs, audio.SizeBytes})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// 打开上游后可能第一次得知总长度，协商时已声明的长度则必须一致。
func Matches(scope Scope, expected, actual pluginproto.OnlineAudio) bool {
	if actual.Validate() != nil || expected.SizeBytes > 0 && actual.SizeBytes > 0 && expected.SizeBytes != actual.SizeBytes {
		return false
	}
	if expected.SampleRateHz > 0 && actual.SampleRateHz > 0 && expected.SampleRateHz != actual.SampleRateHz {
		return false
	}
	actual.SizeBytes = expected.SizeBytes
	if expected.Version == "" {
		actual.Version = ""
	}
	return Key(scope, expected) == Key(scope, actual)
}

// Package fault contains shared SDK error values and request identifiers.
package fault

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("secure random generator unavailable")
	}
	return hex.EncodeToString(b)
}

type Error struct {
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	RequestID  string          `json:"requestId"`
	RetryAfter *int            `json:"retryAfter"`
	Scraper    *ScraperFailure `json:"scraper,omitempty"`
}

// ScraperFailure 标明本次查询失败的位置。冷却中的状态码来自此前的平台响应。
type ScraperFailure struct {
	PluginID       string `json:"pluginId"`
	Reason         string `json:"reason"`
	UpstreamStatus int    `json:"upstreamStatus,omitempty"`
}

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func Fail(code, message string) error { return &Error{Code: code, Message: message} }

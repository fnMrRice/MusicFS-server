// Package domain contains shared value types without infrastructure dependencies.
package domain

import (
	"crypto/rand"
	"encoding/hex"
)

type UserID string
type SourceTrackID string
type AssetID string
type JobID string
type Revision int64

func NewID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("secure random generator unavailable")
	}
	return hex.EncodeToString(b)
}

type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"requestId"`
	RetryAfter *int   `json:"retryAfter"`
}

func (e *Error) Error() string        { return e.Code + ": " + e.Message }
func Fail(code, message string) error { return &Error{Code: code, Message: message} }

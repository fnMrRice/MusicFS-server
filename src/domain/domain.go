// Package domain contains shared value types without infrastructure dependencies.
package domain

import "github.com/fnMrRice/MusicFS-server/sdk/fault"

type UserID string
type SourceTrackID string
type AssetID string
type JobID string
type Revision int64

type Error = fault.Error

func NewID() string                   { return fault.NewID() }
func Fail(code, message string) error { return fault.Fail(code, message) }

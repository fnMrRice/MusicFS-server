package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type RecordingCoverCandidate struct {
	ObjectID           string `json:"objectId"`
	SourceTrackID      string `json:"sourceTrackId"`
	ProviderInstanceID string `json:"providerInstanceId"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
}

type RecordingCover struct {
	RecordingID    string                    `json:"recordingId"`
	Revision       int64                     `json:"revision"`
	CoverObjectID  *string                   `json:"coverObjectId"`
	ManualObjectID *string                   `json:"manualObjectId"`
	Candidates     []RecordingCoverCandidate `json:"candidates"`
}

// SelectRecordingCoverRequest 的 ObjectID 为 nil 时取消人工偏好，恢复按实际像素自动选择。
type SelectRecordingCoverRequest struct {
	RecordingID      string  `json:"recordingId"`
	ExpectedRevision int64   `json:"expectedRevision"`
	ObjectID         *string `json:"objectId"`
}

type RecordingCoverRepository interface {
	RecordingCover(context.Context, string, string) (RecordingCover, error)
	SelectRecordingCover(context.Context, string, SelectRecordingCoverRequest, string) (RecordingCover, error)
}

func (s *Service) RecordingCover(ctx context.Context, owner, id string) (RecordingCover, error) {
	repo, ok := s.repo.(RecordingCoverRepository)
	if !ok {
		return RecordingCover{}, domain.Fail("UNSUPPORTED", "Recording covers are unavailable")
	}
	return repo.RecordingCover(ctx, owner, id)
}

func (s *Service) SelectRecordingCover(ctx context.Context, owner string, request SelectRecordingCoverRequest, key string) (RecordingCover, error) {
	repo, ok := s.repo.(RecordingCoverRepository)
	if !ok {
		return RecordingCover{}, domain.Fail("UNSUPPORTED", "Recording covers are unavailable")
	}
	return repo.SelectRecordingCover(ctx, owner, request, key)
}

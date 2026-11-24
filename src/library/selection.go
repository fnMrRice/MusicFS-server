package library

import (
	"context"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// TrackSelection 固定列表范围；批量模式用排除集合表达尚未加载的已选行。
type TrackSelection struct {
	Batch              bool     `json:"batch"`
	SourceTrackIDs     []string `json:"sourceTrackIds,omitempty"`
	Exclude            []string `json:"exclude,omitempty"`
	ProviderInstanceID string   `json:"providerInstanceId,omitempty"`
	GroupBy            string   `json:"groupBy,omitempty"`
	LibraryRevision    string   `json:"libraryRevision,omitempty"`
	MatchingRevision   *int64   `json:"matchingRevision,omitempty"`
}

// SelectionRepository 在一次事务中验证范围、所有者和修订，并在存储边界内裁剪为播放引用。
type SelectionRepository interface {
	ResolveTrackSelection(context.Context, string, TrackSelection) (TrackSelectionResult, error)
}

// SelectionTrack 只包含本地播放队列需要的引用与展示信息。
type SelectionTrack struct {
	ID              string   `json:"sourceTrackId"`
	ProviderID      string   `json:"providerInstanceId"`
	DisplayName     string   `json:"displayName"`
	Title           *string  `json:"title"`
	Artists         []string `json:"artists"`
	DurationMs      *int64   `json:"durationMs"`
	Availability    string   `json:"availability"`
	AssetID         string   `json:"assetId"`
	CoverObjectID   *string  `json:"coverObjectId,omitempty"`
	OnlineCoverPath string   `json:"onlineCoverPath,omitempty"`
}

type TrackSelectionResult struct {
	Items []SelectionTrack `json:"items"`
	Total int              `json:"total"`
}

func ValidateTrackSelection(selection TrackSelection) error {
	invalid := func() error { return domain.Fail("INVALID_ARGUMENT", "Invalid track selection") }
	if selection.GroupBy != "" && selection.GroupBy != "recording" ||
		selection.Batch && (selection.LibraryRevision == "" || len(selection.SourceTrackIDs) != 0) ||
		!selection.Batch && (len(selection.SourceTrackIDs) == 0 || len(selection.Exclude) != 0) ||
		selection.GroupBy == "recording" && selection.MatchingRevision == nil ||
		selection.MatchingRevision != nil && *selection.MatchingRevision < 0 {
		return invalid()
	}
	if selection.LibraryRevision != "" {
		revision, err := strconv.ParseInt(selection.LibraryRevision, 10, 64)
		if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != selection.LibraryRevision {
			return invalid()
		}
	}
	validID := func(id string) bool {
		return id != "" && len(id) <= 128 && utf8.ValidString(id) && strings.IndexFunc(id, func(r rune) bool {
			return unicode.IsControl(r) || unicode.IsSpace(r)
		}) < 0
	}
	if selection.ProviderInstanceID != "" && !validID(selection.ProviderInstanceID) {
		return invalid()
	}
	for _, ids := range [][]string{selection.SourceTrackIDs, selection.Exclude} {
		if len(ids) > 100000 {
			return invalid()
		}
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if !validID(id) || seen[id] {
				return invalid()
			}
			seen[id] = true
		}
	}
	return nil
}

func (s *Service) SelectTracks(ctx context.Context, owner string, selection TrackSelection) (TrackSelectionResult, error) {
	result := TrackSelectionResult{Items: []SelectionTrack{}}
	if err := ValidateTrackSelection(selection); err != nil {
		return result, err
	}
	if selection.ProviderInstanceID != "" {
		if err := s.AuthorizeProvider(ctx, owner, selection.ProviderInstanceID); err != nil {
			return result, err
		}
	}
	repo, ok := s.repo.(SelectionRepository)
	if !ok {
		return result, domain.Fail("UNSUPPORTED", "Track selection is unavailable")
	}
	return repo.ResolveTrackSelection(ctx, owner, selection)
}

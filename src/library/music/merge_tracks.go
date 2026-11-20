package music

import "github.com/fnMrRice/MusicFS-server/src/library/works"

type MergeTrackMember struct {
	TrackID          string `json:"trackId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// MergeTrackMetadata 是用户确认的最终资料。专辑关系和来源由合并事务维护。
type MergeTrackMetadata struct {
	TrackFields
	ArtistNames []string `json:"artistNames"`
}

func (m MergeTrackMetadata) Normalize() (MergeTrackMetadata, error) {
	if m.LocalizedTitles == nil || m.ArtistNames == nil {
		return m, invalid()
	}
	input, err := (TrackInput{TrackFields: m.TrackFields, ArtistNames: m.ArtistNames}).Normalize()
	return MergeTrackMetadata{TrackFields: input.TrackFields, ArtistNames: input.ArtistNames}, err
}

type MergeTracksInput struct {
	TargetTrackID            string              `json:"targetTrackId"`
	Tracks                   []MergeTrackMember  `json:"tracks"`
	ExpectedAlbumRevision    int64               `json:"expectedAlbumRevision"`
	ExpectedMatchingRevision int64               `json:"expectedMatchingRevision"`
	Metadata                 *MergeTrackMetadata `json:"metadata,omitempty"`
}

func (input MergeTracksInput) Validate() error {
	if input.ExpectedAlbumRevision < 1 || input.ExpectedMatchingRevision < 0 || len(input.Tracks) < 2 || len(input.Tracks) > 64 {
		return invalid()
	}
	seen := make(map[string]bool, len(input.Tracks))
	for _, member := range input.Tracks {
		if !works.ValidID(member.TrackID) || member.ExpectedRevision < 1 || seen[member.TrackID] {
			return invalid()
		}
		seen[member.TrackID] = true
	}
	if !seen[input.TargetTrackID] {
		return invalid()
	}
	return nil
}

package music

import "github.com/fnMrRice/MusicFS-server/src/library/works"

type MergeTrackMember struct {
	TrackID          string `json:"trackId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type MergeTracksInput struct {
	TargetTrackID            string             `json:"targetTrackId"`
	Tracks                   []MergeTrackMember `json:"tracks"`
	ExpectedAlbumRevision    int64              `json:"expectedAlbumRevision"`
	ExpectedMatchingRevision int64              `json:"expectedMatchingRevision"`
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

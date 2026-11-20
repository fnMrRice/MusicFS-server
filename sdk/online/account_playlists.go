package online

import "time"

// PlaylistTrackAddition 将写入限定在关联时选择的平台账号。
type PlaylistTrackAddition struct {
	AccountID  string `json:"accountId"`
	ExternalID string `json:"externalId"`
	TrackID    string `json:"trackId"`
}

type PlaylistsRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`
}

type PlaylistsPage struct {
	Items       []Playlist `json:"items"`
	NextCursor  *string    `json:"nextCursor"`
	Consistency string     `json:"consistency"`
	ObservedAt  time.Time  `json:"observedAt"`
}

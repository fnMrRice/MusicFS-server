package online

import "time"

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

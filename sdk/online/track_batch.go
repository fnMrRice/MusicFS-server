package online

import "time"

type TrackLookup struct {
	ExternalID string  `json:"externalId"`
	Track      *Track  `json:"track"`
	ErrorCode  *string `json:"errorCode"`
}

type TrackBatch struct {
	Items []TrackLookup `json:"items"`
}

type PlaylistTracksRequest struct {
	ExternalID string `json:"externalId"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
}

type PlaylistTrack struct {
	Position int `json:"position"`
	TrackLookup
}

type PlaylistTracksPage struct {
	Playlist       Playlist        `json:"playlist"`
	Items          []PlaylistTrack `json:"items"`
	NextCursor     *string         `json:"nextCursor"`
	MetadataDigest string          `json:"metadataDigest"`
	Complete       bool            `json:"complete"`
	Consistency    string          `json:"consistency"`
	ObservedAt     time.Time       `json:"observedAt"`
}

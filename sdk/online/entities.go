package online

type Album struct {
	ExternalID string   `json:"externalId"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	TrackCount *int64   `json:"trackCount"`
	CoverURL   *string  `json:"coverUrl"`
	SourceURL  string   `json:"sourceUrl"`
}

type Artist struct {
	ExternalID string  `json:"externalId"`
	Name       string  `json:"name"`
	AlbumCount *int64  `json:"albumCount"`
	TrackCount *int64  `json:"trackCount"`
	ImageURL   *string `json:"imageUrl"`
	SourceURL  string  `json:"sourceUrl"`
}

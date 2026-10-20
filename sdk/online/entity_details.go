package online

type EntityPageRequest struct {
	ExternalID string `json:"externalId"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
}

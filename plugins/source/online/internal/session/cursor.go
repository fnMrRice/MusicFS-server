package session

// Cursor contains the runtime's signed paging observation.
type Cursor struct {
	Binding string `json:"b"`
	Offset  int    `json:"o"`
	Expires int64  `json:"e"`
	Version string `json:"v,omitempty"`
}

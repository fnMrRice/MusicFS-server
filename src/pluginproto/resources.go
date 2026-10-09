package pluginproto

type Entry struct {
	RootID          string `json:"rootId"`
	RelativePath    string `json:"relativePath"`
	FileIdentity    string `json:"fileIdentity"`
	ObservedVersion string `json:"observedVersion"`
	SizeBytes       int64  `json:"sizeBytes"`
	MIME            string `json:"mime"`
	DurationMs      *int64 `json:"durationMs"`
}

type EntriesRequest struct {
	RootID string `json:"rootId"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}
type EntriesPage struct {
	Items        []Entry  `json:"items"`
	NextCursor   string   `json:"nextCursor,omitempty"`
	RootIdentity string   `json:"rootIdentity"`
	Issues       []string `json:"issues"`
}
type StateRequest struct {
	Key      string `json:"key"`
	Revision int64  `json:"revision"`
	Value    []byte `json:"value,omitempty"`
}
type StateValue struct {
	Revision int64  `json:"revision"`
	Value    []byte `json:"value,omitempty"`
}

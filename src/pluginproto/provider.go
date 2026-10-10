package pluginproto

type ScanRequest struct {
	RootID     string    `json:"rootId"`
	ScanID     string    `json:"scanId"`
	Generation int64     `json:"generation"`
	Cursor     string    `json:"cursor,omitempty"`
	Limit      int       `json:"limit"`
	Scope      ScanScope `json:"scope"`
	PlanDigest string    `json:"planDigest"`
	GrantID    string    `json:"grantId"`
}

type Observation struct {
	ExternalID      string  `json:"externalId"`
	RootID          string  `json:"rootId"`
	RelativePath    string  `json:"relativePath"`
	SourceRef       string  `json:"sourceRef"`
	ObservedVersion string  `json:"observedVersion"`
	DisplayName     string  `json:"displayName"`
	SizeBytes       int64   `json:"sizeBytes"`
	MIME            string  `json:"mime"`
	Codec           *string `json:"codec"`
	DurationMs      *int64  `json:"durationMs"`
	BitrateBps      *int64  `json:"bitrateBps"`
	BitrateMode     *string `json:"bitrateMode"`
	ProviderSHA512  *string `json:"providerSha512"`
}

type ScanPage struct {
	PlanDigest   string        `json:"planDigest"`
	ScanID       string        `json:"scanId"`
	Generation   int64         `json:"generation"`
	Page         int           `json:"page"`
	Items        []Observation `json:"items"`
	NextCursor   string        `json:"nextCursor,omitempty"`
	Complete     bool          `json:"complete"`
	RootIdentity string        `json:"rootIdentity"`
	Issues       []string      `json:"issues"`
}

type ResolveRequest struct {
	ExternalID      string `json:"externalId"`
	ExpectedVersion string `json:"expectedVersion"`
}

type Resource struct {
	ResourceRef     string `json:"resourceRef"`
	RootID          string `json:"rootId"`
	RelativePath    string `json:"relativePath"`
	ObservedVersion string `json:"observedVersion"`
	SizeBytes       int64  `json:"sizeBytes"`
	MIME            string `json:"mime"`
	Range           bool   `json:"range"`
}

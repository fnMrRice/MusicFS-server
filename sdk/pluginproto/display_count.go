package pluginproto

// DisplayCountRequest exposes selected root identities, never filesystem paths.
type DisplayCountRequest struct {
	InstanceID string   `json:"instanceId"`
	RootIDs    []string `json:"rootIds"`
}

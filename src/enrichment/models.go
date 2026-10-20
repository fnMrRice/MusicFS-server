// Package enrichment defines content descriptors. Lyrics and image bodies are
// transferred separately and never belong in persisted control-message JSON.
package enrichment

import "github.com/fnMrRice/MusicFS-server/sdk/pluginproto"

const MaxResources = 32
const MaxCandidates = 64

type Target struct {
	SourceTrackID string `json:"sourceTrackId"`
	AssetID       string `json:"assetId"`
	AssetVersion  string `json:"assetVersion"`
	// Revision is the media-selection revision, not the audio asset revision.
	Revision int64 `json:"revision"`
}

type Resource struct {
	ResourceRef string `json:"resourceRef"`
	Version     string `json:"version"`
	Kind        string `json:"kind"`
	SizeBytes   int64  `json:"sizeBytes"`
	Match       string `json:"match"`
}

type Query struct {
	Title   string   `json:"title"`
	Artists []string `json:"artists"`
}

type SearchRequest struct {
	Context   pluginproto.Context `json:"context"`
	Target    Target              `json:"target"`
	Fields    []string            `json:"fields"`
	Languages []string            `json:"languages"`
	Query     *Query              `json:"query,omitempty"`
	Resources []Resource          `json:"resources"`
}

type Candidate struct {
	CandidateID     string  `json:"candidateId"`
	Target          Target  `json:"target"`
	Field           string  `json:"field"`
	Language        string  `json:"language"`
	Format          string  `json:"format"`
	MIME            string  `json:"mime"`
	SizeBytes       int64   `json:"sizeBytes"`
	SHA256          string  `json:"sha256"`
	Origin          string  `json:"origin"`
	ResourceRef     string  `json:"resourceRef"`
	ResourceVersion string  `json:"resourceVersion"`
	Basis           string  `json:"basis"`
	Status          string  `json:"status"`
	FailureCode     *string `json:"failureCode"`
}

type Issue struct {
	ResourceRef string `json:"resourceRef"`
	Code        string `json:"code"`
}

type SearchResult struct {
	Candidates []Candidate `json:"candidates"`
	Issues     []Issue     `json:"issues"`
}

type FetchRequest struct {
	Context     pluginproto.Context `json:"context"`
	Target      Target              `json:"target"`
	CandidateID string              `json:"candidateId"`
	OutputRef   string              `json:"outputRef"`
}

type FetchResult struct {
	Candidate  Candidate `json:"candidate"`
	ContentRef string    `json:"contentRef"`
}

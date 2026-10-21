package enrichment

import "time"

// ScrapeDocument is an immutable, complete structured attempt. Resource numbers
// are document-local identifiers, never reusable worker capabilities or paths.
type ScrapeDocument struct {
	Schema       string             `json:"schema"`
	JobID        string             `json:"jobId"`
	Target       Target             `json:"target"`
	Targets      []Target           `json:"targets"`
	Generation   int64              `json:"generation"`
	Request      JobRequest         `json:"request"`
	Resources    []DocumentResource `json:"resources"`
	SourceIssues []DocumentIssue    `json:"sourceIssues"`
	Attempts     []ScraperAttempt   `json:"attempts"`
	Outcome      DocumentOutcome    `json:"outcome"`
	StartedAt    time.Time          `json:"startedAt"`
	FinishedAt   time.Time          `json:"finishedAt"`
}
type DocumentOutcome struct {
	Candidates  []PreviewCandidate `json:"candidates"`
	Issues      []JobIssue         `json:"issues"`
	FailureCode *string            `json:"failureCode"`
}
type DocumentResource struct {
	Number    int    `json:"number"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"sizeBytes"`
	Match     string `json:"match"`
}
type DocumentIssue struct {
	ResourceNumber int    `json:"resourceNumber"`
	Code           string `json:"code"`
}
type DocumentCandidate struct {
	CandidateID     string  `json:"candidateId"`
	Target          Target  `json:"target"`
	Field           string  `json:"field"`
	Language        string  `json:"language"`
	Format          string  `json:"format"`
	MIME            string  `json:"mime"`
	SizeBytes       int64   `json:"sizeBytes"`
	SHA256          string  `json:"sha256"`
	Origin          string  `json:"origin"`
	ResourceNumber  int     `json:"resourceNumber"`
	ResourceVersion string  `json:"resourceVersion"`
	Basis           string  `json:"basis"`
	Status          string  `json:"status"`
	FailureCode     *string `json:"failureCode"`
}
type DocumentSearch struct {
	Candidates []DocumentCandidate `json:"candidates"`
	Issues     []DocumentIssue     `json:"issues"`
}
type DocumentFetch struct {
	CandidateID string            `json:"candidateId"`
	State       string            `json:"state"`
	Code        *string           `json:"code"`
	Preview     *PreviewCandidate `json:"preview"`
}
type ScraperAttempt struct {
	InstanceID    string          `json:"instanceId"`
	PluginID      string          `json:"pluginId"`
	PluginVersion string          `json:"pluginVersion"`
	State         string          `json:"state"`
	FailureCode   *string         `json:"failureCode"`
	Search        *DocumentSearch `json:"search"`
	Fetches       []DocumentFetch `json:"fetches"`
}

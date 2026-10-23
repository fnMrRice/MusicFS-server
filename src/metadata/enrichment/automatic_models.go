package enrichment

import (
	"context"
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

// AutomaticPolicy preserves instance order: it is precedence, not a set.
type AutomaticPolicy struct {
	Enabled          bool     `json:"enabled" yaml:"enabled"`
	InstancePriority []string `json:"instancePriority" yaml:"instancePriority"`
	Fields           []string `json:"fields" yaml:"fields"`
	Languages        []string `json:"languages" yaml:"languages"`
}

func (p AutomaticPolicy) Normalize() (AutomaticPolicy, error) {
	if len(p.InstancePriority) > 4 || len(p.Fields) > 2 || len(p.Languages) > 16 || (p.Enabled && len(p.Fields) == 0) {
		return p, domain.Fail("INVALID_ARGUMENT", "Invalid automatic metadata policy")
	}
	if _, err := jobIDs(p.InstancePriority); err != nil {
		return p, err
	}
	p.InstancePriority = append([]string{}, p.InstancePriority...)
	fields := p.Fields
	if len(fields) == 0 {
		fields = []string{"lyrics"}
	}
	r, err := (JobRequest{SourceTrackIDs: []string{"policy"}, Fields: fields, Languages: p.Languages}).Normalize()
	if err != nil {
		return p, err
	}
	if len(p.Fields) > 0 {
		p.Fields = r.Fields
	} else {
		p.Fields = []string{}
	}
	p.Languages = r.Languages
	return p, nil
}

type AutomationAction struct {
	Field            string  `json:"field"`
	Language         string  `json:"language"`
	Status           string  `json:"status"`
	Reason           string  `json:"reason"`
	CandidateID      *string `json:"candidateId"`
	BindingID        *string `json:"bindingId"`
	ExpectedRevision int64   `json:"-"`
}

type AutomationResult struct {
	TriggerID     string             `json:"triggerId"`
	ScanJobID     string             `json:"scanJobId"`
	SourceTrackID string             `json:"sourceTrackId"`
	AssetID       string             `json:"assetId"`
	AssetVersion  string             `json:"assetVersion"`
	Reason        string             `json:"reason"`
	State         string             `json:"state"`
	JobID         *string            `json:"jobId"`
	FailureCode   *string            `json:"failureCode"`
	Actions       []AutomationAction `json:"actions"`
}

type AutomationPage struct {
	Items      []AutomationResult `json:"items"`
	NextCursor *string            `json:"nextCursor"`
}

// AutomationWork is internal, including the durable policy snapshot.
type AutomationWork struct {
	AutomationResult
	Owner  string
	Policy *AutomaticPolicy
}

type AutomationRepository interface {
	NextAutomation(context.Context) (AutomationWork, error)
	PrepareAutomation(context.Context, string, AutomaticPolicy) error
	QueueAutomaticEnrichment(context.Context, string, string, JobRequest) (Job, bool, error)
	AttachAutomationJob(context.Context, string, string) error
	FinishAutomation(context.Context, string, string, *string) error
	AutomationAction(context.Context, string, AutomationAction) (AutomationAction, error)
	FinishAutomationAction(context.Context, string, AutomationAction) error
	ScanAutomationResults(context.Context, string, string, string, int) (AutomationPage, error)
	ApplyAutomaticEnrichment(context.Context, string, string, string, int64, string, bool, mediastore.Guard) (mediastore.Selection, error)
}

// EligibleAutomatic 仅自动选用带单曲归属证据的候选，不接受目录级封面。
func EligibleAutomatic(c PreviewCandidate, p AutomaticPolicy) bool {
	return c.Status == "available" && c.PreviewObjectID != nil && slices.Contains(p.Fields, c.Field) &&
		slices.Contains(p.InstancePriority, c.InstanceID) && TrackSpecificCandidate(c)
}

// TrackSpecificCandidate 判断候选是否来自单曲本身或同名旁车资源。
func TrackSpecificCandidate(c PreviewCandidate) bool {
	return c.Origin == "embedded" || c.Origin == "sidecar" && c.Basis == "exactStem" ||
		c.Origin == "container" && c.Field == "artwork" && c.Basis == "containerEmbedded"
}

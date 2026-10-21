package matching

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type LibrarySnapshot struct {
	Revision string
	Sources  []CandidateSource
}
type Run struct {
	Owner            string
	Key              string
	LibraryRevision  string
	SettingsRevision int64
	Job              settings.Job
}
type AutomationRepository interface {
	MatchingOwners(context.Context) ([]string, error)
	MatchingLibrary(context.Context, string) (LibrarySnapshot, error)
	MatchingRun(context.Context, string) (Run, error)
	SaveMatchingRun(context.Context, Run) error
	MatchingAction(context.Context, string, string) (*settings.Job, error)
}

// Detector 提供声明式设置和候选选择；宿主保留授权、任务和人工合并的所有权。
type Detector struct {
	ID        string
	Schema    pluginsettings.Schema
	Scorer    func(settings.Values) candidates.Scorer
	Automatic func(settings.Values) bool
	Buckets   func(CandidateSource) []string
}

package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

type Track struct {
	ID                  string                `json:"sourceTrackId"`
	ProviderID          string                `json:"providerInstanceId"`
	DisplayName         string                `json:"displayName"`
	Title               *string               `json:"title"`
	Artists             []string              `json:"artists"`
	Album               *string               `json:"album"`
	CoverObjectID       *string               `json:"coverObjectId"`
	Genre               *string               `json:"genre"`
	Year                *int64                `json:"year"`
	SampleRateHz        *int64                `json:"sampleRateHz"`
	DurationMs          *int64                `json:"durationMs"`
	Codec               *string               `json:"codec"`
	Container           *string               `json:"container"`
	BitrateBps          *int64                `json:"bitrateBps"`
	Availability        string                `json:"availability"`
	AssetID             string                `json:"assetId"`
	Asset               *identification.Asset `json:"asset"`
	RecordingID         *string               `json:"recordingId"`
	ManualSourceTrackID *string               `json:"manualSourceTrackId"`
	Versions            []Track               `json:"versions,omitempty"`
	Owner               string                `json:"-"`
	ExternalID          string                `json:"-"`
	RootID              string                `json:"-"`
	Path                string                `json:"-"`
	Version             string                `json:"-"`
	MIME                string                `json:"-"`
	Size                int64                 `json:"-"`
}
type Provider struct {
	ID            string                      `json:"id"`
	Name          string                      `json:"name"`
	Plugin        *pluginproto.PluginIdentity `json:"plugin,omitempty"`
	Capabilities  []string                    `json:"capabilities"`
	Availability  string                      `json:"availability"`
	AccountState  string                      `json:"accountState"`
	ScanState     string                      `json:"scanState"`
	Configuration *pluginproto.Configuration  `json:"configuration"`
	Roots         []ProviderRoot              `json:"roots"`
}
type ProviderRoot struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	WriteBack *RootWriteBack `json:"writeBack,omitempty"`
}
type RootWriteBack struct {
	BoundPath     string   `json:"-"`
	BoundIdentity string   `json:"-"`
	Enabled       bool     `json:"enabled"`
	Available     bool     `json:"available"`
	Formats       []string `json:"formats"`
	Fields        []string `json:"fields"`
	Replacement   string   `json:"replacement"`
}
type Page struct {
	Items            []Track `json:"items"`
	NextCursor       *string `json:"nextCursor"`
	LibraryRevision  string  `json:"libraryRevision"`
	MatchingRevision *int64  `json:"matchingRevision,omitempty"`
}
type Observation struct {
	pluginproto.Observation
	Container           *string
	Title               *string
	Artists             []string
	Album               *string
	Genre               *string
	Year                *int64
	SampleRateHz        *int64
	Evidence            []identification.Evidence
	ProviderObservation *pluginproto.Observation
	ProbeStatus         string
}
type Repository interface {
	Publish(context.Context, jobs.Job, ScanPublication) error
	ListTracks(context.Context, string, string, string, int) (Page, error)
	FindTrack(context.Context, string, string) (Track, error)
	ProviderScanState(context.Context, string, string) string
}

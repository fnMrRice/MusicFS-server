package library

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type Track struct {
	ID           string                `json:"sourceTrackId"`
	ProviderID   string                `json:"providerInstanceId"`
	DisplayName  string                `json:"displayName"`
	Title        *string               `json:"title"`
	Artists      []string              `json:"artists"`
	DurationMs   *int64                `json:"durationMs"`
	Codec        *string               `json:"codec"`
	Container    *string               `json:"container"`
	BitrateBps   *int64                `json:"bitrateBps"`
	Availability string                `json:"availability"`
	AssetID      string                `json:"assetId"`
	Asset        *identification.Asset `json:"asset"`
	Owner        string                `json:"-"`
	ExternalID   string                `json:"-"`
	RootID       string                `json:"-"`
	Path         string                `json:"-"`
	Version      string                `json:"-"`
	MIME         string                `json:"-"`
	Size         int64                 `json:"-"`
}
type Provider struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	Availability string   `json:"availability"`
	AccountState string   `json:"accountState"`
	ScanState    string   `json:"scanState"`
}
type Page struct {
	Items           []Track `json:"items"`
	NextCursor      *string `json:"nextCursor"`
	LibraryRevision string  `json:"libraryRevision"`
}
type Observation struct {
	pluginproto.Observation
	Container           *string
	Title               *string
	Artists             []string
	Evidence            []identification.Evidence
	ProviderObservation *pluginproto.Observation
	ProbeStatus         string
}
type Repository interface {
	Publish(context.Context, jobs.Job, []Observation) error
	ListTracks(context.Context, string, string, string, int) (Page, error)
	FindTrack(context.Context, string, string) (Track, error)
	ProviderScanState(context.Context, string, string) string
}

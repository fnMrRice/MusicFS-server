package workpackages

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

type Source struct {
	ProviderID string       `json:"providerInstanceId"`
	Name       string       `json:"name"`
	Roots      []SourceRoot `json:"roots"`
}
type SourceRoot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Selection struct {
	ProviderID string `json:"providerInstanceId"`
	RootID     string `json:"rootId"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
}
type Counts struct {
	Directories int `json:"directories"`
	Audio       int `json:"audio"`
	Attachments int `json:"attachments"`
}
type Preview struct {
	ID        string    `json:"previewId"`
	WorkID    string    `json:"workId"`
	ExpiresAt time.Time `json:"expiresAt"`
	Package   Package   `json:"package"`
	Counts    Counts    `json:"counts"`
	Warnings  []string  `json:"warnings"`
}
type pendingPreview struct {
	view         Preview
	owner        string
	workRevision int64
	snapshot     library.PackageSnapshot
}

type Service struct {
	repo       Repository
	works      works.Repository
	library    *library.Service
	organizers map[string]contenttype.PackageOrganizer
	mu         sync.Mutex
	previews   map[string]pendingPreview
	operations chan struct{}
}

func New(repo Repository, records works.Repository, sources *library.Service, organizers map[string]contenttype.PackageOrganizer) *Service {
	copy := make(map[string]contenttype.PackageOrganizer, len(organizers))
	for id, organizer := range organizers {
		copy[id] = organizer
	}
	return &Service{repo: repo, works: records, library: sources, organizers: copy, previews: map[string]pendingPreview{}, operations: make(chan struct{}, 4)}
}

func (s *Service) Sources(ctx context.Context, owner, workID string) ([]Source, error) {
	if _, err := s.works.Work(ctx, owner, workID); err != nil {
		return nil, err
	}
	result := []Source{}
	for _, provider := range s.library.Providers(ctx, owner) {
		if provider.Plugin == nil || provider.Plugin.ID != "musicfs.plugin.source.localfiles" || provider.Availability != "available" {
			continue
		}
		roots := []SourceRoot{}
		for _, root := range provider.Roots {
			roots = append(roots, SourceRoot{ID: root.ID, Name: root.Name})
		}
		if len(roots) > 0 {
			result = append(result, Source{ProviderID: provider.ID, Name: provider.Name, Roots: roots})
		}
	}
	return result, nil
}

func (s *Service) Browse(ctx context.Context, owner, workID string, input Selection) (library.PackageDirectories, error) {
	if _, err := s.works.Work(ctx, owner, workID); err != nil {
		return library.PackageDirectories{}, err
	}
	if err := validateLocation(input); err != nil {
		return library.PackageDirectories{}, err
	}
	return s.library.BrowsePackageDirectories(ctx, owner, input.ProviderID, input.RootID, input.Path)
}

func (s *Service) List(ctx context.Context, owner, workID string) ([]Package, error) {
	if _, err := s.works.Work(ctx, owner, workID); err != nil {
		return nil, err
	}
	items, err := s.repo.WorkPackages(ctx, owner, workID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if err = s.populateTracks(ctx, owner, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) populateTracks(ctx context.Context, owner string, pkg *Package) error {
	for i := range pkg.Nodes {
		node := &pkg.Nodes[i]
		node.Track = nil
		if node.Kind != "audio" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		track, err := s.library.Track(ctx, owner, node.SourceTrackID)
		if err != nil {
			var failure *domain.Error
			if !errors.As(err, &failure) || failure.Code != "NOT_FOUND" {
				return err
			}
			continue
		}
		if err == nil && track.AssetID == node.AssetID && track.Version == node.Version {
			node.Track = &track
		}
	}
	return nil
}

func validateLocation(input Selection) error {
	if !works.ValidID(input.ProviderID) || !works.ValidID(input.RootID) || !ValidPath(input.Path, true) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid package selection")
	}
	return nil
}

func (s *Service) begin() (func(), error) {
	select {
	case s.operations <- struct{}{}:
		return func() { <-s.operations }, nil
	default:
		return nil, domain.Fail("RATE_LIMITED", "Too many package operations")
	}
}

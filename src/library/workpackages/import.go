package workpackages

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func (s *Service) Import(ctx context.Context, owner, workID, previewID, key string) (Package, error) {
	if !works.ValidID(previewID) || !works.ValidID(key) || len(key) < 8 {
		return Package{}, domain.Fail("INVALID_ARGUMENT", "Invalid package import operation")
	}
	if _, err := s.works.Work(ctx, owner, workID); err != nil {
		return Package{}, err
	}
	if previous, ok, err := s.repo.ReplayWorkPackage(ctx, owner, workID, previewID, key); err != nil || ok {
		if err == nil {
			err = s.populateTracks(ctx, owner, &previous)
		}
		return previous, err
	}
	s.mu.Lock()
	pending, ok := s.previews[previewID]
	s.mu.Unlock()
	if !ok || pending.owner != owner || pending.view.WorkID != workID {
		return Package{}, domain.Fail("NOT_FOUND", "Package preview not found")
	}
	if time.Now().After(pending.view.ExpiresAt) {
		return Package{}, domain.Fail("CONFLICT", "Package preview expired; preview again")
	}
	end, err := s.begin()
	if err != nil {
		return Package{}, err
	}
	defer end()
	pkg := pending.view.Package
	// 节点切片在确认时补入观察身份，不能改写供其他重试读取的预览。
	pkg.Nodes = append([]Node{}, pkg.Nodes...)
	lease, err := s.library.HoldRead(ctx, owner, pkg.ProviderID)
	if err != nil {
		return Package{}, err
	}
	defer lease.Close()
	ctx = lease.Context()
	snapshot, err := s.library.PackageSourceSnapshot(ctx, owner, pkg.ProviderID, pkg.RootID, pkg.RelativePath)
	if err != nil {
		return Package{}, err
	}
	if snapshot.Signature != pending.snapshot.Signature || snapshot.ConfigRevision != pending.snapshot.ConfigRevision || snapshot.RootIdentity != pending.snapshot.RootIdentity {
		return Package{}, domain.Fail("RESOURCE_CHANGED", "Package changed after preview; preview again")
	}
	observations, err := s.library.PackageSourceObservations(ctx, owner, snapshot)
	if err != nil {
		return Package{}, err
	}
	byPath := make(map[string]int, len(pkg.Nodes))
	for i, node := range pkg.Nodes {
		byPath[node.SourcePath] = i
	}
	for _, observation := range observations {
		i, ok := byPath[observation.RelativePath]
		if !ok || pkg.Nodes[i].Kind != "audio" {
			return Package{}, domain.Fail("RESOURCE_CHANGED", "Package audio observation changed")
		}
		pkg.Nodes[i].ExternalID = observation.ExternalID
		pkg.Nodes[i].MIME = observation.MIME
	}
	input := Publication{Package: pkg, ExpectedWorkRevision: pending.workRevision, ExpectedPackageRevision: pkg.Revision, Observations: observations}
	if err = ValidatePublication(input); err != nil {
		return Package{}, err
	}
	result, err := s.repo.PublishWorkPackage(ctx, owner, input, previewID, key)
	if err != nil {
		return Package{}, err
	}
	s.mu.Lock()
	delete(s.previews, previewID)
	s.mu.Unlock()
	err = s.populateTracks(ctx, owner, &result)
	return result, err
}

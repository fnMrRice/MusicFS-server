package workpackages

import (
	"context"
	"path"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Service) Preview(ctx context.Context, owner, workID string, input Selection) (Preview, error) {
	if !validKind(input.Kind) {
		return Preview{}, domain.Fail("INVALID_ARGUMENT", "Invalid work package kind")
	}
	if err := validateLocation(input); err != nil {
		return Preview{}, err
	}
	work, err := s.works.Work(ctx, owner, workID)
	if err != nil {
		return Preview{}, err
	}
	organizer := s.organizers[work.ContentType]
	if organizer == nil {
		return Preview{}, domain.Fail("UNSUPPORTED", "Content type cannot import packages")
	}
	end, err := s.begin()
	if err != nil {
		return Preview{}, err
	}
	defer end()
	snapshot, err := s.library.PackageSourceSnapshot(ctx, owner, input.ProviderID, input.RootID, input.Path)
	if err != nil {
		return Preview{}, err
	}
	pkg, counts, err := planPackage(workID, snapshot, organizer)
	if err != nil {
		return Preview{}, err
	}
	pkg.Kind = input.Kind
	packages, err := s.repo.WorkPackages(ctx, owner, workID)
	if err != nil {
		return Preview{}, err
	}
	for _, previous := range packages {
		if previous.ProviderID == pkg.ProviderID && previous.RootID == pkg.RootID && previous.RelativePath == pkg.RelativePath {
			pkg.ID, pkg.Revision = previous.ID, previous.Revision
			break
		}
	}
	view := Preview{ID: domain.NewID(), WorkID: workID, ExpiresAt: time.Now().UTC().Add(15 * time.Minute), Package: pkg, Counts: counts, Warnings: []string{}}
	if counts.Audio == 0 {
		view.Warnings = append(view.Warnings, "目录中没有识别到可播放音频，将仅导入目录和附件。")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, item := range s.previews {
		if time.Now().After(item.view.ExpiresAt) {
			delete(s.previews, id)
		} else if item.owner == owner {
			count++
		}
	}
	if len(s.previews) >= 32 || count >= 4 {
		return Preview{}, domain.Fail("RATE_LIMITED", "Too many pending package previews")
	}
	s.previews[view.ID] = pendingPreview{view: view, owner: owner, workRevision: work.Revision, snapshot: snapshot}
	return view, nil
}

func planPackage(workID string, snapshot library.PackageSnapshot, organizer contenttype.PackageOrganizer) (Package, Counts, error) {
	pkg := Package{WorkID: workID, ProviderID: snapshot.ProviderID, RootID: snapshot.RootID, RelativePath: snapshot.RelativePath,
		RootIdentity: snapshot.RootIdentity, ConfigRevision: snapshot.ConfigRevision, Signature: snapshot.Signature, Nodes: []Node{}}
	counts := Counts{}
	entries := make([]contenttype.PackageEntry, 0, len(snapshot.Entries))
	byPath := make(map[string]library.PackageEntry, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		entries = append(entries, contenttype.PackageEntry{Path: entry.Path, Kind: entry.Kind})
		byPath[entry.Path] = entry
	}
	ordered, err := organizer.OrderPackage(entries)
	if err != nil || len(ordered) != len(entries) || len(entries) == 0 || len(entries) > MaxNodes {
		return pkg, counts, domain.Fail("INVALID_ARGUMENT", "Invalid package organization")
	}
	seen := map[string]bool{}
	for order, name := range ordered {
		entry, ok := byPath[name]
		if !ok || seen[name] {
			return pkg, counts, domain.Fail("INVALID_ARGUMENT", "Package organizer changed source paths")
		}
		seen[name] = true
		parent := path.Dir(name)
		if name == "." {
			parent = ""
			pkg.Name = entry.Name
		}
		pkg.Nodes = append(pkg.Nodes, Node{ID: name, ParentID: parent, Path: name, Name: entry.Name, Kind: entry.Kind, Order: order,
			MIME: entry.MIME, SizeBytes: entry.SizeBytes, Version: entry.Version, SourcePath: entry.SourcePath, AttachmentKind: entry.AttachmentKind})
		switch entry.Kind {
		case "directory":
			counts.Directories++
		case "audio":
			counts.Audio++
		case "attachment":
			counts.Attachments++
		}
	}
	return pkg, counts, nil
}

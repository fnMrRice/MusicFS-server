package workpackages

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

// OpenAttachment 每次读取都重新检查作品归属、当前来源授权和导入时的文件版本。
func (s *Service) OpenAttachment(ctx context.Context, owner, workID, packageID, nodeID string) (Node, *sourceaccess.StableReader, func(), error) {
	if _, err := s.works.Work(ctx, owner, workID); err != nil {
		return Node{}, nil, nil, err
	}
	pkg, err := s.repo.WorkPackage(ctx, owner, workID, packageID)
	if err != nil {
		return Node{}, nil, nil, err
	}
	for _, node := range pkg.Nodes {
		if node.ID != nodeID || node.Kind != "attachment" {
			continue
		}
		stable, release, err := s.library.PackageSourceOpen(ctx, owner, pkg.ProviderID, pkg.RootID, node.SourcePath, node.Version)
		if err != nil {
			return Node{}, nil, nil, err
		}
		return node, stable, release, nil
	}
	return Node{}, nil, nil, domain.Fail("NOT_FOUND", "Package attachment not found")
}

// Package workpackages 管理作品的本地副本、完整目录树及来源关联。
package workpackages

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
)

const (
	MaxNodes         = 5000
	MaxPackages      = 64
	MaxDepth         = 32
	MaxPathBytes     = 4096
	MaxDocumentBytes = 8 << 20
)

type Package struct {
	ID             string    `json:"packageId"`
	WorkID         string    `json:"workId"`
	Kind           string    `json:"kind"`
	ProviderID     string    `json:"providerInstanceId"`
	RootID         string    `json:"rootId"`
	RelativePath   string    `json:"relativePath"`
	Name           string    `json:"name"`
	Revision       int64     `json:"revision"`
	ImportedAt     time.Time `json:"importedAt"`
	Nodes          []Node    `json:"nodes"`
	Owner          string    `json:"-"`
	RootIdentity   string    `json:"-"`
	ConfigRevision string    `json:"-"`
	Signature      string    `json:"-"`
}

type Node struct {
	ID             string         `json:"nodeId"`
	ParentID       string         `json:"parentId"`
	Path           string         `json:"path"`
	Name           string         `json:"name"`
	Kind           string         `json:"kind"`
	Order          int            `json:"order"`
	MIME           string         `json:"mime"`
	SizeBytes      int64          `json:"sizeBytes"`
	SourceTrackID  string         `json:"sourceTrackId"`
	AssetID        string         `json:"assetId"`
	AttachmentKind string         `json:"attachmentKind"`
	Track          *library.Track `json:"track"`
	Version        string         `json:"-"`
	SourcePath     string         `json:"-"`
	ExternalID     string         `json:"-"`
}

// Publication 只接收宿主校验后的完整快照，观察与目录节点在同一事务发布。
type Publication struct {
	Package                 Package
	ExpectedWorkRevision    int64
	ExpectedPackageRevision int64
	Observations            []library.Observation
}

type Repository interface {
	PublishWorkPackage(context.Context, string, Publication, string, string) (Package, error)
	WorkPackages(context.Context, string, string) ([]Package, error)
	WorkPackage(context.Context, string, string, string) (Package, error)
	ReplayWorkPackage(context.Context, string, string, string, string) (Package, bool, error)
}

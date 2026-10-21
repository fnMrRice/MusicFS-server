package mediastore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// DerivedMetadata contains references only. Structured scrape results belong in
// the immutable document file, never in this publication/index summary.
type DerivedMetadata struct {
	Transform      string `json:"transform"`
	ParentObjectID string `json:"parentObjectId,omitempty"`
	ParentSHA256   string `json:"parentSha256,omitempty"`
	JobID          string `json:"jobId,omitempty"`
	SourceTrackID  string `json:"sourceTrackId,omitempty"`
	AssetID        string `json:"assetId,omitempty"`
	AssetVersion   string `json:"assetVersion,omitempty"`
	Generation     int64  `json:"generation,omitempty"`
}

func (m DerivedMetadata) Key() (string, error) {
	valid := m.Transform == "thumbnail256/v1" && objectID.MatchString(m.ParentObjectID) && len(m.ParentSHA256) == 64 && m.JobID == "" && m.SourceTrackID == "" && m.AssetID == "" && m.AssetVersion == "" && m.Generation == 0
	valid = valid || m.Transform == "scrape-document/v1" && objectID.MatchString(m.JobID) && m.SourceTrackID != "" && m.AssetID != "" && m.AssetVersion != "" && len(m.SourceTrackID) <= 256 && len(m.AssetID) <= 256 && len(m.AssetVersion) <= 256 && m.Generation > 0 && m.ParentObjectID == "" && m.ParentSHA256 == ""
	if !valid {
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid derived media relationship")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

type DerivedObject struct {
	Object
	Metadata  DerivedMetadata `json:"metadata"`
	State     string          `json:"state"`
	CreatedAt time.Time       `json:"createdAt"`
}

type DerivedRepository interface {
	DerivedObject(context.Context, string, string) (DerivedObject, error)
	FindDerived(context.Context, string, string) (DerivedObject, error)
	ListScrapeDocuments(context.Context, string, string) ([]DerivedObject, error)
	InvalidateDerived(context.Context, string, string) error
}

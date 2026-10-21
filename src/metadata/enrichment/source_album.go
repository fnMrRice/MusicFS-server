package enrichment

import "github.com/fnMrRice/MusicFS-server/src/core/domain"

// SourceAlbumRequest selects an explicit membership revision. Queueing expands
// it in the transaction that snapshots each asset and media selection revision.
type SourceAlbumRequest struct {
	SourceAlbumID    string `json:"sourceAlbumId"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

func (r SourceAlbumRequest) Validate() error {
	if r.ExpectedRevision < 1 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid source album revision")
	}
	_, err := jobIDs([]string{r.SourceAlbumID})
	return err
}

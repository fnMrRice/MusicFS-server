package sqlite

import (
	"context"
	"database/sql"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

// The original album request is the idempotency identity; expanded targets
// are immutable queue rows, captured under the same transaction as this read.
func enrichmentSources(ctx context.Context, tx *sql.Tx, owner string, request enrichment.JobRequest) ([]string, error) {
	sources := request.SourceTrackIDs
	if request.SourceAlbum != nil {
		album, err := readSourceAlbum(ctx, tx, owner, request.SourceAlbum.SourceAlbumID)
		if err != nil {
			return nil, err
		}
		if album.Revision != request.SourceAlbum.ExpectedRevision {
			return nil, domain.Fail("CONFLICT", "Source album membership revision changed")
		}
		sources = album.SourceTrackIDs
		if len(sources) < 1 || len(sources) > enrichment.MaxJobSources {
			return nil, domain.Fail("INVALID_ARGUMENT", "Source album must contain between 1 and 64 tracks for metadata jobs")
		}
	}
	if err := visibleSources(ctx, tx, owner, sources); err != nil {
		return nil, err
	}
	return sources, nil
}

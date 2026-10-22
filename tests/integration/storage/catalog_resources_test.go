package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
)

func TestCatalogCategoryRejectsExcessiveDirectoryResources(t *testing.T) {
	for name, path := range map[string]string{
		"depth": strings.Repeat("d/", 257) + "song.flac",
		"bytes": strings.Repeat("d", 4097) + "/song.flac",
	} {
		t.Run(name, func(t *testing.T) {
			s, _, tracks := matchingFixture(t, 1)
			ctx := context.Background()
			if _, err := s.DB.ExecContext(ctx, "UPDATE tracks SET path=? WHERE id=?", path, tracks[0].ID); err != nil {
				t.Fatal(err)
			}
			_, err := s.CreateCatalogQuery(ctx, "admin", catalog.Query{Type: "categories", CategoryKind: "folder"}, 1, "")
			matchingError(t, err, "RATE_LIMITED")
			_, err = s.CatalogFacets(ctx, "admin", "")
			matchingError(t, err, "RATE_LIMITED")
			var snapshots int
			if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_queries").Scan(&snapshots); err != nil || snapshots != 0 {
				t.Fatalf("rejected directory query retained a snapshot: %d %v", snapshots, err)
			}
		})
	}
}

func TestCatalogCategoryRejectsMoreCategoriesThanTracksBudget(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	track := tracks[0]
	track.Artists = make([]string, catalog.MaxItems+1)
	for i := range track.Artists {
		track.Artists[i] = fmt.Sprintf("Artist-%06d", i)
	}
	categoryUpdateTrack(t, s, track)
	_, err := s.CreateCatalogQuery(ctx, "admin", catalog.Query{Type: "categories", CategoryKind: "artist"}, 1, "")
	matchingError(t, err, "RATE_LIMITED")
	var snapshots int
	if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_queries").Scan(&snapshots); err != nil || snapshots != 0 {
		t.Fatalf("rejected category query retained a snapshot: %d %v", snapshots, err)
	}
}

func TestCatalogCategorySharedDeepDirectoryKeepsCountsAndBreadcrumbs(t *testing.T) {
	s := evidenceStore(t)
	observations := make([]library.Observation, 100)
	for i := range observations {
		observations[i] = evidenceObservation(fmt.Sprintf("song-%03d", i), "v1")
	}
	publishEvidenceTracks(t, s, observations...)
	ctx := context.Background()
	segments := make([]string, 256)
	for i := range segments {
		segments[i] = fmt.Sprintf("d%03d", i)
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE tracks SET path=? || '/' || path", strings.Join(segments, "/")); err != nil {
		t.Fatal(err)
	}
	service := catalog.New(s)
	roots := categorySearch(t, service, catalog.Query{CategoryKind: "folder"})
	if len(roots) != 1 || roots[0].TrackCount != len(observations) || len(roots[0].Breadcrumbs) != 1 {
		t.Fatal("shared paths changed root counts", roots)
	}
	facets, err := service.Facets(ctx, "admin", "")
	if err != nil || len(facets.Directories) != len(segments)+1 {
		t.Fatalf("shared directory index duplicated ancestors: %d %v", len(facets.Directories), err)
	}
	var leaf string
	for _, directory := range facets.Directories {
		if directory.Name == segments[len(segments)-1] {
			leaf = directory.ID
		}
	}
	if leaf == "" {
		t.Fatal("deepest allowed directory is missing")
	}
	detail, err := service.Search(ctx, "admin", catalog.Query{Type: "categories", CategoryKind: "folder", CategoryID: leaf}, "", 1)
	if err != nil || len(detail.Items) != 1 {
		t.Fatalf("deep directory detail failed: %+v %v", detail, err)
	}
	var category catalog.Category
	if err = json.Unmarshal(detail.Items[0], &category); err != nil {
		t.Fatal(err)
	}
	if len(category.Breadcrumbs) != len(segments)+1 || category.Breadcrumbs[0].ID != roots[0].ID || category.Breadcrumbs[len(segments)].ID != leaf || category.TrackCount != len(observations) {
		t.Fatal("deep directory breadcrumbs or counts changed", category)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = service.Search(cancelled, "admin", catalog.Query{Type: "categories", CategoryKind: "folder"}, "", 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled category query continued: %v", err)
	}
}

package catalog

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }

func (q Query) Normalize() (Query, error) {
	q.Q = strings.TrimSpace(q.Q)
	q.Format = strings.ToLower(strings.TrimSpace(q.Format))
	if q.Type == "" {
		q.Type = "tracks"
	}
	if q.Sort == "" {
		q.Sort = "name"
	}
	if q.Order == "" {
		q.Order = "asc"
	}
	if q.Type != "tracks" && q.Type != "albums" && q.Type != "artists" && q.Type != "categories" {
		return q, domain.Fail("INVALID_ARGUMENT", "Invalid catalog result type")
	}
	if q.CategoryKind != "" && !slices.Contains([]string{"artist", "album", "genre", "folder"}, q.CategoryKind) {
		return q, invalidQuery()
	}
	if q.Type == "categories" && q.CategoryKind == "" ||
		q.CategoryKind != "" && q.Type != "categories" && q.Type != "tracks" ||
		q.CategoryID != "" && q.CategoryKind == "" ||
		q.Type == "tracks" && q.CategoryKind != "" && q.CategoryID == "" ||
		q.ParentID != "" && (q.Type != "categories" || q.CategoryKind != "folder" || q.CategoryID != "") {
		return q, invalidQuery()
	}
	if !validText(q.Q, 256) || !validText(q.Format, 64) {
		return q, invalidQuery()
	}
	for _, values := range []*[]string{&q.Genres, &q.Formats, &q.Availability, &q.UnknownFields} {
		if len(*values) > 32 {
			return q, invalidQuery()
		}
		*values = append([]string{}, (*values)...)
		for i, value := range *values {
			value = strings.TrimSpace(value)
			if !validText(value, 256) || value == "" {
				return q, invalidQuery()
			}
			(*values)[i] = strings.ToLower(value)
		}
		slices.Sort(*values)
		*values = slices.Compact(*values)
	}
	for _, value := range q.Availability {
		if value != "available" && value != "missing" {
			return q, invalidQuery()
		}
	}
	for _, value := range q.UnknownFields {
		if !slices.Contains([]string{"artist", "album", "genre", "year", "sampleRateHz", "sampleratehz", "bitrate", "format"}, value) {
			return q, invalidQuery()
		}
	}
	for _, values := range []*[]int64{&q.Years, &q.SampleRatesHz} {
		if len(*values) > 128 {
			return q, invalidQuery()
		}
		*values = append([]int64{}, (*values)...)
		for _, value := range *values {
			if value < 1 || value > 1000000 {
				return q, invalidQuery()
			}
		}
		slices.Sort(*values)
		*values = slices.Compact(*values)
	}
	for _, id := range []string{q.ProviderInstanceID, q.ArtistID, q.SourceAlbumID, q.RootID, q.DirectoryID, q.CategoryID, q.ParentID} {
		if !validText(id, 128) || strings.IndexFunc(id, unicode.IsSpace) >= 0 {
			return q, invalidQuery()
		}
	}
	if q.Order != "asc" && q.Order != "desc" {
		return q, invalidQuery()
	}
	allowed := q.Sort == "name" || (q.Type == "tracks" && (q.Sort == "title" || q.Sort == "artist" || q.Sort == "album" || q.Sort == "duration" || q.Sort == "bitrate"))
	if !allowed {
		return q, invalidQuery()
	}
	if q.MinBitrateBps != nil && *q.MinBitrateBps < 0 || q.MaxBitrateBps != nil && *q.MaxBitrateBps < 0 || q.MinBitrateBps != nil && q.MaxBitrateBps != nil && *q.MinBitrateBps > *q.MaxBitrateBps {
		return q, invalidQuery()
	}
	return q, nil
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}
func invalidQuery() error { return domain.Fail("INVALID_ARGUMENT", "Invalid catalog query") }
func PageLimit(limit int) (int, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > MaxPageSize {
		return 0, domain.Fail("INVALID_ARGUMENT", "limit must be between 1 and 200")
	}
	return limit, nil
}
func (s *Service) Create(ctx context.Context, owner string, input CreateRequest, key string) (Page, error) {
	if len(key) < 8 || len(key) > 128 || !validText(key, 128) {
		return Page{}, domain.Fail("INVALID_ARGUMENT", "Idempotency-Key must contain 8 to 128 characters")
	}
	q, err := input.Query.Normalize()
	if err != nil {
		return Page{}, err
	}
	limit, err := PageLimit(input.Limit)
	if err != nil {
		return Page{}, err
	}
	return s.repo.CreateCatalogQuery(ctx, owner, q, limit, key)
}
func (s *Service) Search(ctx context.Context, owner string, query Query, cursor string, limit int) (Page, error) {
	q, err := query.Normalize()
	if err != nil {
		return Page{}, err
	}
	limit, err = PageLimit(limit)
	if err != nil {
		return Page{}, err
	}
	if cursor != "" {
		return s.repo.CatalogQueryPage(ctx, owner, "", cursor, limit, &q)
	}
	return s.repo.CreateCatalogQuery(ctx, owner, q, limit, "")
}
func (s *Service) Page(ctx context.Context, owner, id, cursor string, limit int) (Page, error) {
	limit, err := PageLimit(limit)
	if err != nil {
		return Page{}, err
	}
	return s.repo.CatalogQueryPage(ctx, owner, id, cursor, limit, nil)
}
func (s *Service) Cancel(ctx context.Context, owner, id string) error {
	return s.repo.CancelCatalogQuery(ctx, owner, id)
}
func (s *Service) Facets(ctx context.Context, owner, provider string) (Facets, error) {
	q, err := (Query{ProviderInstanceID: provider}).Normalize()
	if err != nil {
		return Facets{}, err
	}
	return s.repo.CatalogFacets(ctx, owner, q.ProviderInstanceID)
}
func (s *Service) Artist(ctx context.Context, owner, id string) (Artist, error) {
	return s.repo.CatalogArtist(ctx, owner, id)
}

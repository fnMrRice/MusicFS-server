package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/catalog"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (s *Server) RegisterCatalog(service *catalog.Service) {
	s.Handle("GET /api/v1/library/search", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		query, cursor, limit, err := catalogSearchRequest(r.URL.Query())
		if err != nil {
			return err
		}
		page, err := service.Search(r.Context(), a.User.ID, query, cursor, limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("POST /api/v1/library/queries", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var input struct {
			Query *catalog.Query `json:"query"`
			Limit int            `json:"limit"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Query == nil {
			return domain.Fail("INVALID_ARGUMENT", "Catalog query is required")
		}
		page, err := service.Create(r.Context(), a.User.ID, catalog.CreateRequest{Query: *input.Query, Limit: input.Limit}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, page)
	})
	s.Handle("GET /api/v1/library/queries/{id}", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		query := r.URL.Query()
		if err := catalogParameters(query, map[string]bool{"cursor": false, "limit": false}); err != nil {
			return err
		}
		limit, err := catalogLimit(query)
		if err != nil {
			return err
		}
		page, err := service.Page(r.Context(), a.User.ID, r.PathValue("id"), query.Get("cursor"), limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("DELETE /api/v1/library/queries/{id}", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if err := service.Cancel(r.Context(), a.User.ID, r.PathValue("id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	s.Handle("GET /api/v1/library/facets", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		query := r.URL.Query()
		if err := catalogParameters(query, map[string]bool{"providerInstanceId": false}); err != nil {
			return err
		}
		result, err := service.Facets(r.Context(), a.User.ID, query.Get("providerInstanceId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("GET /api/v1/source-artists/{id}", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		result, err := service.Artist(r.Context(), a.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}

func catalogSearchRequest(values url.Values) (catalog.Query, string, int, error) {
	q := catalog.Query{}
	allowed := map[string]bool{}
	for _, key := range []string{"type", "providerInstanceId", "q", "artistId", "sourceAlbumId", "rootId", "directoryId", "format", "favoriteOnly", "availableOnly", "minBitrateBps", "maxBitrateBps", "sort", "order", "cursor", "limit"} {
		allowed[key] = false
	}
	for _, key := range []string{"genres", "formats", "years", "sampleRatesHz", "availability", "unknownFields"} {
		allowed[key] = true
	}
	if err := catalogParameters(values, allowed); err != nil {
		return q, "", 0, err
	}
	q.Type, q.ProviderInstanceID, q.Q = values.Get("type"), values.Get("providerInstanceId"), values.Get("q")
	q.ArtistID, q.SourceAlbumID, q.RootID, q.DirectoryID = values.Get("artistId"), values.Get("sourceAlbumId"), values.Get("rootId"), values.Get("directoryId")
	q.Format, q.Sort, q.Order = values.Get("format"), values.Get("sort"), values.Get("order")
	q.Genres, q.Formats, q.Availability, q.UnknownFields = values["genres"], values["formats"], values["availability"], values["unknownFields"]
	for name, dest := range map[string]*bool{"favoriteOnly": &q.FavoriteOnly, "availableOnly": &q.AvailableOnly} {
		if values.Has(name) {
			if values.Get(name) != "true" && values.Get(name) != "false" {
				return q, "", 0, invalidCatalogParameter()
			}
			*dest = values.Get(name) == "true"
		}
	}
	for name, dest := range map[string]**int64{"minBitrateBps": &q.MinBitrateBps, "maxBitrateBps": &q.MaxBitrateBps} {
		if values.Has(name) {
			n, err := strconv.ParseInt(values.Get(name), 10, 64)
			if err != nil {
				return q, "", 0, invalidCatalogParameter()
			}
			*dest = &n
		}
	}
	for name, dest := range map[string]*[]int64{"years": &q.Years, "sampleRatesHz": &q.SampleRatesHz} {
		for _, value := range values[name] {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return q, "", 0, invalidCatalogParameter()
			}
			*dest = append(*dest, n)
		}
	}
	limit, err := catalogLimit(values)
	return q, values.Get("cursor"), limit, err
}
func catalogParameters(values url.Values, allowed map[string]bool) error {
	for name, items := range values {
		multiple, ok := allowed[name]
		if !ok || !multiple && len(items) != 1 || len(items) > 128 {
			return invalidCatalogParameter()
		}
		for _, value := range items {
			if len(value) > 4096 {
				return invalidCatalogParameter()
			}
		}
	}
	return nil
}
func catalogLimit(values url.Values) (int, error) {
	if !values.Has("limit") {
		return 50, nil
	}
	limit, err := strconv.Atoi(values.Get("limit"))
	if err != nil || limit < 1 || limit > catalog.MaxPageSize {
		return 0, invalidCatalogParameter()
	}
	return limit, nil
}
func invalidCatalogParameter() error {
	return domain.Fail("INVALID_ARGUMENT", "Invalid catalog query parameter")
}

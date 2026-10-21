package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
)

type albumPaginationInput struct {
	after, provider string
	sequence        int64
	limit           int
}

func albumPagination(r *http.Request, providerAllowed, sequence bool) (albumPaginationInput, error) {
	page := albumPaginationInput{limit: 50}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid album query")
	}
	for key, values := range query {
		if len(values) != 1 || (key != "after" && key != "limit" && !(providerAllowed && key == "providerInstanceId")) {
			return page, domain.Fail("INVALID_ARGUMENT", "Unknown or duplicate album query")
		}
	}
	if values, ok := query["limit"]; ok {
		page.limit, err = strconv.Atoi(values[0])
		if err != nil || page.limit < 1 || page.limit > albums.MaxPageSize {
			return page, domain.Fail("INVALID_ARGUMENT", "Album limit is outside supported range")
		}
	}
	page.after, page.provider = query.Get("after"), query.Get("providerInstanceId")
	if page.provider != "" && !albums.ValidID(page.provider) {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid provider identifier")
	}
	if sequence && page.after != "" {
		page.sequence, err = strconv.ParseInt(page.after, 10, 64)
		if err != nil || page.sequence < 0 {
			return page, domain.Fail("INVALID_ARGUMENT", "Invalid album change sequence")
		}
	} else if page.after != "" && !albums.ValidID(page.after) {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid album cursor")
	}
	return page, nil
}

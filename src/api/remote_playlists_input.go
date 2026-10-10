package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

type remotePage struct {
	after         string
	query         string
	offset, limit int
}

func remotePagination(r *http.Request, snapshot bool, searchable ...bool) (remotePage, error) {
	p := remotePage{limit: 50}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return p, playlists.Invalid()
	}
	for key, values := range query {
		if len(values) != 1 || (key != "limit" && !(snapshot && key == "offset") && !(!snapshot && key == "after") && !(len(searchable) > 0 && searchable[0] && key == "q")) {
			return p, playlists.Invalid()
		}
		switch key {
		case "q":
			p.query = values[0]
		case "limit":
			n, e := remoteInteger(values[0], 1, playlists.MaxPageSize)
			if e != nil {
				return p, e
			}
			p.limit = int(n)
		case "offset":
			n, e := remoteInteger(values[0], 0, playlists.MaxSnapshotEntries)
			if e != nil {
				return p, e
			}
			p.offset = int(n)
		case "after":
			if !playlists.ValidID(values[0]) {
				return p, playlists.Invalid()
			}
			p.after = values[0]
		}
	}
	return p, nil
}

func remoteInteger(raw string, minimum, maximum int64) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < minimum || n > maximum || strconv.FormatInt(n, 10) != raw {
		return 0, playlists.Invalid()
	}
	return n, nil
}

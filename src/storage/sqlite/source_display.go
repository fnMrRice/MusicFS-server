package sqlite

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/src/sourcedisplay"
)

func (s *Store) SourceDisplay(ctx context.Context, owner string, refs []playlists.SourceRef) (sourcedisplay.Page, error) {
	if err := sourcedisplay.Validate(refs); err != nil {
		return sourcedisplay.Page{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return sourcedisplay.Page{}, err
	}
	defer tx.Rollback()
	found := map[string]sourcedisplay.Item{}
	for _, kind := range []string{"local", "remote"} {
		ids := []any{owner}
		for _, ref := range refs {
			if kind == "local" && ref.SourceTrackID != nil {
				ids = append(ids, *ref.SourceTrackID)
			}
			if kind == "remote" && ref.RemoteSourceID != nil {
				ids = append(ids, *ref.RemoteSourceID)
			}
		}
		if len(ids) == 1 {
			continue
		}
		table := "tracks"
		if kind == "remote" {
			table = "remote_sources"
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)-1), ",")
		rows, err := tx.QueryContext(ctx, "SELECT id,document FROM "+table+" WHERE owner=? AND id IN ("+placeholders+")", ids...)
		if err != nil {
			return sourcedisplay.Page{}, err
		}
		for rows.Next() {
			var id string
			var raw []byte
			if err = rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return sourcedisplay.Page{}, err
			}
			item := sourcedisplay.Item{}
			if kind == "local" {
				var track library.Track
				err = json.Unmarshal(raw, &track)
				item.Local = &track
			} else {
				var remote playlists.RemoteSource
				err = json.Unmarshal(raw, &remote)
				item.RemoteSource = &remote
			}
			if err != nil {
				rows.Close()
				return sourcedisplay.Page{}, err
			}
			found[kind+":"+id] = item
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return sourcedisplay.Page{}, err
		}
	}
	page := sourcedisplay.Page{Items: []sourcedisplay.Item{}}
	for _, ref := range refs {
		item, ok := found[ref.Key()]
		item.Source = ref
		if !ok {
			code := "NOT_FOUND"
			item.ErrorCode = &code
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

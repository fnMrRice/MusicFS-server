package online

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

const brainzSite = "https://musicbrainz.org"
const brainzCandidates = 5

var (
	brainzID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	isrcID   = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{3}[0-9]{7}$`)
)

type brainzPlugin struct{ client *http.Client }

func MusicBrainz(client *http.Client) scraper.Plugin { return brainzPlugin{client} }

func (brainzPlugin) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: "musicfs.scraper.musicbrainz", Type: "scraper", Name: "MusicBrainz", Available: true,
		Fields: []string{"trackDetails", "albumDetails", "albumTracks", "artistDetails"}}
}

func (p brainzPlugin) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	entity, inc, err := brainzField(field)
	if err != nil {
		return nil, err
	}
	rows, err := p.entities(ctx, entity, inc, q)
	if err != nil {
		return nil, err
	}
	items := make([]scraper.Item, 0, len(rows))
	for _, row := range rows {
		// 搜索结果只给出每碟曲数；必须用发行版 ID 取得完整曲目表。
		if field == "albumTracks" && q.ExternalID == "" {
			row, err = p.lookup(ctx, entity, inc, row.ID)
			if errors.Is(err, errCatalogMissing) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		item := row.item(entity)
		if field == "albumTracks" {
			item.Tracks = row.trackList()
			if len(item.Tracks) == 0 {
				continue
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func brainzField(field string) (entity, inc string, err error) {
	switch field {
	case "albumDetails":
		return "release", "artist-credits+labels+release-groups", nil
	case "albumTracks":
		return "release", "artist-credits+recordings", nil
	case "trackDetails":
		return "recording", "artist-credits+isrcs+releases", nil
	case "artistDetails":
		return "artist", "aliases", nil
	default:
		return "", "", fault.Fail("INVALID_ARGUMENT", "unsupported MusicBrainz field")
	}
}

func (p brainzPlugin) entities(ctx context.Context, entity, inc string, q scraper.Query) ([]brainzEntity, error) {
	id := strings.TrimSpace(q.ExternalID)
	var search string
	if id != "" && brainzID.MatchString(id) {
		row, err := p.lookup(ctx, entity, inc, id)
		if errors.Is(err, errCatalogMissing) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []brainzEntity{row}, nil
	}
	if id != "" {
		isrc := strings.ToUpper(strings.ReplaceAll(id, "-", ""))
		if entity != "recording" || !isrcID.MatchString(isrc) {
			return nil, fault.Fail("INVALID_ARGUMENT", "invalid MusicBrainz identifier")
		}
		search = brainzTerm("isrc", isrc)
	} else {
		var err error
		search, err = brainzSearchTerms(entity, q)
		if err != nil {
			return nil, err
		}
	}
	var result struct{ Artists, Releases, Recordings []brainzEntity }
	params := url.Values{"query": {search}, "limit": {"5"}}
	if err := p.get(ctx, entity, params, &result); err != nil {
		return nil, err
	}
	rows := result.Recordings
	switch entity {
	case "release":
		rows = result.Releases
	case "artist":
		rows = result.Artists
	}
	valid := rows[:0]
	for _, row := range rows {
		if brainzID.MatchString(row.ID) && (row.Title != "" || row.Name != "") {
			valid = append(valid, row)
		}
		if len(valid) == brainzCandidates {
			break
		}
	}
	return valid, nil
}

func brainzSearchTerms(entity string, q scraper.Query) (string, error) {
	name := strings.TrimSpace(q.Title)
	switch entity {
	case "release":
		if album := strings.TrimSpace(q.Album); album != "" {
			name = album
		}
	case "artist":
		if artist := strings.TrimSpace(q.Artist); artist != "" {
			name = artist
		}
	}
	if name == "" {
		return "", fault.Fail("INVALID_ARGUMENT", "catalog search requires a title or external identifier")
	}
	terms := []string{brainzTerm(entity, name)}
	if entity != "artist" && strings.TrimSpace(q.Artist) != "" {
		terms = append(terms, brainzTerm("artist", q.Artist))
	}
	if entity == "release" && strings.TrimSpace(q.Country) != "" {
		terms = append(terms, brainzTerm("country", q.Country))
	}
	if entity == "recording" && strings.TrimSpace(q.Album) != "" {
		terms = append(terms, brainzTerm("release", q.Album))
	}
	return strings.Join(terms, " AND "), nil
}

func brainzTerm(field, value string) string {
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(strings.TrimSpace(value))
	return field + `:"` + quoted + `"`
}

func (p brainzPlugin) lookup(ctx context.Context, entity, inc, id string) (brainzEntity, error) {
	var row brainzEntity
	err := p.get(ctx, entity+"/"+id, url.Values{"inc": {inc}}, &row)
	if err == nil && (!strings.EqualFold(row.ID, id) || (row.Title == "" && row.Name == "")) {
		err = errors.New("invalid MusicBrainz entity")
	}
	return row, err
}

func (p brainzPlugin) get(ctx context.Context, path string, params url.Values, data any) error {
	params.Set("fmt", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brainzSite+"/ws/2/"+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	if err := brainzPacer.wait(ctx); err != nil {
		return err
	}
	return readCatalogJSON(p.client, req, data)
}

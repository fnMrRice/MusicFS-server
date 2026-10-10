package enrichment

import (
	"regexp"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

var jobLanguage = regexp.MustCompile(`^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$`)

func (r JobRequest) Normalize() (JobRequest, error) {
	if len(r.SourceTrackIDs) < 1 || len(r.SourceTrackIDs) > MaxJobSources || len(r.Fields) < 1 || len(r.Fields) > 2 || len(r.Languages) > 16 || len(r.InstanceIDs) > 4 || len(r.ExcludeInstanceIDs) > 16 {
		return r, domain.Fail("INVALID_ARGUMENT", "Invalid metadata request limits")
	}
	var err error
	r.SourceTrackIDs, err = jobIDs(r.SourceTrackIDs)
	if err != nil {
		return r, err
	}
	r.InstanceIDs, err = jobIDs(r.InstanceIDs)
	if err != nil {
		return r, err
	}
	r.ExcludeInstanceIDs, err = jobIDs(r.ExcludeInstanceIDs)
	if err != nil {
		return r, err
	}
	for _, id := range r.InstanceIDs {
		if slices.Contains(r.ExcludeInstanceIDs, id) {
			return r, domain.Fail("INVALID_ARGUMENT", "Scraper instance is both selected and excluded")
		}
	}
	r.Fields = append([]string{}, r.Fields...)
	slices.Sort(r.Fields)
	for i, field := range r.Fields {
		if (field != "lyrics" && field != "artwork") || (i > 0 && r.Fields[i-1] == field) {
			return r, domain.Fail("INVALID_ARGUMENT", "Invalid metadata fields")
		}
	}
	r.Languages = append([]string{}, r.Languages...)
	for i, language := range r.Languages {
		if len(language) > 35 || !jobLanguage.MatchString(language) {
			return r, domain.Fail("INVALID_ARGUMENT", "Invalid metadata language")
		}
		r.Languages[i] = strings.ToLower(language)
	}
	slices.Sort(r.Languages)
	if len(slices.Compact(append([]string{}, r.Languages...))) != len(r.Languages) {
		return r, domain.Fail("INVALID_ARGUMENT", "Duplicate metadata language")
	}
	if r.Query != nil {
		q := *r.Query
		q.Artists = append([]string{}, q.Artists...)
		if !validText(q.Title, 4096, false) || len(q.Artists) > 32 {
			return r, domain.Fail("INVALID_ARGUMENT", "Invalid metadata query")
		}
		for _, artist := range q.Artists {
			if !validText(artist, 1024, false) {
				return r, domain.Fail("INVALID_ARGUMENT", "Invalid metadata artist")
			}
		}
		r.Query = &q
	}
	return r, nil
}

func jobIDs(input []string) ([]string, error) {
	result := append([]string{}, input...)
	slices.Sort(result)
	for i, id := range result {
		if !validText(id, 128, true) || strings.ContainsAny(id, "\x00\r\n/\\") || (i > 0 && result[i-1] == id) {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid or duplicate metadata identifier")
		}
	}
	return result, nil
}

func ContentTarget(target Target) mediastore.ContentTarget {
	return mediastore.ContentTarget{SourceTrackID: target.SourceTrackID, AssetID: target.AssetID, AssetVersion: target.AssetVersion}
}

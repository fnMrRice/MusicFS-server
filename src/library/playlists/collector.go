package playlists

import (
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// ObservationPage is an internal adapter result. Each entry's position is an
// absolute occurrence position; repeated track IDs are legitimate entries.
type ObservationPage struct {
	Observation
	Cursor     string
	NextCursor *string
}
type Collector struct {
	id                        string
	next                      string
	seen                      map[string]bool
	observation               Observation
	started, complete, failed bool
}

func NewCollector(externalID string) (*Collector, error) {
	if !ValidID(externalID) {
		return nil, Invalid()
	}
	return &Collector{id: externalID, seen: map[string]bool{}}, nil
}
func (c *Collector) Add(p ObservationPage) error {
	if c.failed || c.complete || p.Cursor != c.next || c.seen[p.Cursor] {
		c.failed = true
		return domain.Fail("RESOURCE_CHANGED", "Remote playlist cursor repeated or changed")
	}
	c.seen[p.Cursor] = true
	if p.Playlist.ExternalID != c.id || p.ObservedAt.IsZero() {
		c.failed = true
		return Invalid()
	}
	if !c.started {
		c.observation = p.Observation
		if p.Version != nil {
			version := *p.Version
			c.observation.Version = &version
		}
		if p.Playlist.TrackCount != nil {
			count := *p.Playlist.TrackCount
			c.observation.Playlist.TrackCount = &count
		}
		c.observation.Entries = []online.PlaylistEntry{}
		c.started = true
	} else if !reflect.DeepEqual(c.observation.Playlist, p.Playlist) || c.observation.Consistency != p.Consistency || !reflect.DeepEqual(c.observation.Version, p.Version) || p.ObservedAt.Before(c.observation.ObservedAt) {
		c.failed = true
		return domain.Fail("RESOURCE_CHANGED", "Remote playlist metadata or version changed between pages")
	}
	for _, e := range p.Entries {
		if len(c.observation.Entries) >= MaxEntries || e.Position != len(c.observation.Entries) || !ValidID(e.ExternalTrackID) {
			c.failed = true
			return Invalid()
		}
		c.observation.Entries = append(c.observation.Entries, e)
	}
	c.observation.ObservedAt = p.ObservedAt
	if p.NextCursor != nil {
		if p.Complete || !validText(*p.NextCursor, 2048) || c.seen[*p.NextCursor] || len(p.Entries) == 0 {
			c.failed = true
			return domain.Fail("RESOURCE_CHANGED", "Remote playlist pagination is inconsistent")
		}
		c.next = *p.NextCursor
		return nil
	}
	if !p.Complete {
		c.failed = true
		return domain.Fail("TEMPORARY_FAILURE", "Remote playlist observation is incomplete")
	}
	c.observation.Complete = true
	if err := c.observation.Validate(c.id); err != nil {
		c.failed = true
		return err
	}
	c.complete = true
	return nil
}
func (c *Collector) Finish() (Observation, error) {
	if c.failed || !c.complete {
		return Observation{}, domain.Fail("TEMPORARY_FAILURE", "Remote playlist observation did not complete")
	}
	result := c.observation
	result.Entries = append([]online.PlaylistEntry{}, result.Entries...)
	return result, nil
}

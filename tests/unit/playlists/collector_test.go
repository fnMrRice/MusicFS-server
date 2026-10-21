package playlists_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func page(position int, ids ...string) playlists.ObservationPage {
	count := int64(3)
	p := playlists.ObservationPage{Observation: playlists.Observation{PlaylistObservation: online.PlaylistObservation{Playlist: online.Playlist{ExternalID: "123", Title: "Public", TrackCount: &count}, Consistency: online.Unversioned, ObservedAt: time.Unix(1000, 0)}}}
	for i, id := range ids {
		p.Entries = append(p.Entries, online.PlaylistEntry{Position: position + i, ExternalTrackID: id})
	}
	return p
}
func text(s string) *string { return &s }
func TestPlaylistCollectorRetainsDuplicateOccurrences(t *testing.T) {
	c, _ := playlists.NewCollector("123")
	first := page(0, "1", "1")
	first.NextCursor = text("page-2")
	if err := c.Add(first); err != nil {
		t.Fatal(err)
	}
	last := page(2, "2")
	last.Cursor = "page-2"
	last.Complete = true
	if err := c.Add(last); err != nil {
		t.Fatal(err)
	}
	got, err := c.Finish()
	if err != nil || len(got.Entries) != 3 || got.Entries[0].ExternalTrackID != got.Entries[1].ExternalTrackID || got.Entries[1].Position != 1 {
		t.Fatal("duplicate occurrences lost", err)
	}
	got.Entries[0].ExternalTrackID = "changed"
	again, _ := c.Finish()
	if again.Entries[0].ExternalTrackID != "1" {
		t.Fatal("collector exposed mutable state")
	}
}
func TestPlaylistCollectorRejectsIncompleteInconsistentPages(t *testing.T) {
	for _, kind := range []string{"loop", "duplicate", "position", "metadata", "version", "incomplete", "count", "version-alias"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := playlists.NewCollector("123")
			first := page(0, "1", "1")
			first.NextCursor = text("next")
			next := page(2, "2")
			next.Cursor = "next"
			next.Complete = true
			if kind == "version" || kind == "version-alias" {
				first.Consistency = "versioned"
				first.Version = text("v1")
				next.Consistency = "versioned"
				next.Version = text("v2")
			}
			if err := c.Add(first); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "loop":
				next.Complete = false
				next.NextCursor = text("next")
			case "duplicate":
				next = first
			case "position":
				next.Entries[0].Position = 0
			case "metadata":
				next.Playlist.Title = "changed"
			case "incomplete":
				next.Complete = false
			case "count":
				next.Entries = nil
			case "version-alias":
				*first.Version = "v2"
			}
			if err := c.Add(next); err == nil {
				t.Fatal("invalid page accepted")
			}
			if _, err := c.Finish(); err == nil {
				t.Fatal("partial data published")
			}
		})
	}
}
func TestPlaylistObservationDoesNotTrustClaimedMetadataDigest(t *testing.T) {
	p := page(0, "1", "1", "2")
	p.Complete = true
	p.MetadataDigest = "untrusted"
	if err := p.Observation.Validate("123"); err != nil {
		t.Fatal(err)
	}
	one, _ := p.Observation.Digest()
	p.MetadataDigest = "different"
	two, _ := p.Observation.Digest()
	if one != two {
		t.Fatal("digest includes upstream claim")
	}
	p.Entries[0], p.Entries[2] = p.Entries[2], p.Entries[0]
	p.Entries[0].Position = 0
	p.Entries[2].Position = 2
	three, _ := p.Observation.Digest()
	if reflect.DeepEqual(one, three) {
		t.Fatal("digest ignored order")
	}
}

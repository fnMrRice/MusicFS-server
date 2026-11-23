package online_test

import (
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func TestValidateDailySongs(t *testing.T) {
	now := time.Now().UTC()
	bad := fixtureTrack()
	bad.Title = "Incorrect digest"
	for _, tc := range []struct {
		name  string
		page  online.DailySongsPage
		limit int
		valid bool
	}{
		{"empty", online.DailySongsPage{Tracks: []online.Track{}, ObservedAt: now}, 1, true},
		{"missing tracks", online.DailySongsPage{ObservedAt: now}, 1, false},
		{"missing observation time", online.DailySongsPage{Tracks: []online.Track{}}, 1, false},
		{"invalid track", online.DailySongsPage{Tracks: []online.Track{bad}, ObservedAt: now}, 1, false},
		{"too many tracks", online.DailySongsPage{Tracks: []online.Track{fixtureTrack(), fixtureTrack()}, ObservedAt: now}, 1, false},
		{"invalid limit", online.DailySongsPage{Tracks: []online.Track{}, ObservedAt: now}, online.MaxPageItems + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := online.ValidateDailySongs(fixturePolicy(), tc.page, tc.limit); (err == nil) != tc.valid {
				t.Fatalf("unexpected daily song validation result: %v", err)
			}
		})
	}
}

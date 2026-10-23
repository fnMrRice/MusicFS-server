package online_test

import (
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func fixtureTrack() online.Track {
	track := online.Track{
		ExternalID: "42", Title: "Fixture", Artists: []string{"Artist"},
		SourceURL: "https://music.163.com/song?id=42", PlaybackAvailability: "unknown",
	}
	track.MetadataDigest = online.Digest(track)
	return track
}

func TestMetadataValidationRetainsTrustBoundary(t *testing.T) {
	if err := online.ValidateTrack(fixturePolicy(), fixtureTrack()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*online.Track){
		"private source URL": func(track *online.Track) {
			track.SourceURL = "http://127.0.0.1/private"
			track.MetadataDigest = ""
			track.MetadataDigest = online.Digest(*track)
		},
		"changed metadata": func(track *online.Track) { track.Title = "Changed without new digest" },
		"audio availability claim": func(track *online.Track) {
			track.PlaybackAvailability = "available"
			track.MetadataDigest = ""
			track.MetadataDigest = online.Digest(*track)
		},
		"missing artists": func(track *online.Track) { track.Artists = nil },
	} {
		t.Run(name, func(t *testing.T) {
			track := fixtureTrack()
			mutate(&track)
			if err := online.ValidateTrack(fixturePolicy(), track); err == nil {
				t.Fatal("invalid worker metadata was accepted")
			}
		})
	}
	if err := online.ValidateTrack(online.ResourcePolicy{}, fixtureTrack()); err == nil {
		t.Fatal("unknown platform bypassed the official-platform policy")
	}
}

func TestPlaylistDigestRetainsOrderedDuplicateOccurrences(t *testing.T) {
	count := int64(3)
	cover := "https://p1.music.126.net/image.jpg"
	playlist := online.Playlist{
		ExternalID: "91", Title: "Fixture", TrackCount: &count, CoverURL: &cover,
		SourceURL: "https://music.163.com/playlist?id=91",
	}
	entries := []online.PlaylistEntry{{Position: 0, ExternalTrackID: "42"}, {Position: 1, ExternalTrackID: "42"}, {Position: 2, ExternalTrackID: "43"}}
	observation := online.PlaylistObservation{
		Playlist: playlist, Entries: entries, Complete: true, Consistency: online.Unversioned,
		ObservedAt: time.Now().UTC(), MetadataDigest: online.PlaylistDigest(playlist, entries),
	}
	if err := online.ValidateObservation(fixturePolicy(), observation, "91"); err != nil {
		t.Fatalf("valid duplicate occurrence rejected: %v", err)
	}
	cover = "https://p2.music.126.net/image.jpg"
	if got := online.PlaylistDigest(playlist, entries); got != observation.MetadataDigest {
		t.Fatal("CDN routing invalidated the membership digest")
	}
	observation.Entries[1].ExternalTrackID, observation.Entries[2].ExternalTrackID = "43", "42"
	if online.ValidateObservation(fixturePolicy(), observation, "91") == nil {
		t.Fatal("reordered observations reused the old digest")
	}
}

func fixturePolicy() online.ResourcePolicy {
	return online.ResourcePolicy{
		Tracks:     []online.ReferenceRule{{IDPattern: `^([1-9][0-9]{0,19})$`, URLTemplate: "https://music.163.com/song?id=${1}"}},
		Playlists:  []online.ReferenceRule{{IDPattern: `^([1-9][0-9]{0,19})$`, URLTemplate: "https://music.163.com/playlist?id=${1}"}},
		ImageHosts: []string{"*.music.126.net"},
	}
}

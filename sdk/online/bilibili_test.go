package online_test

import (
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func bilibiliTrack(id, source string) online.Track {
	track := online.Track{ExternalID: id, Title: "Public fixture", Artists: []string{"Uploader"}, SourceURL: source, PlaybackAvailability: "unknown"}
	track.MetadataDigest = online.Digest(track)
	return track
}

func TestBilibiliCanonicalTrackIdentities(t *testing.T) {
	for _, tc := range []struct{ id, source string }{
		{"video:BV1xx411c7mD:11", "https://www.bilibili.com/video/BV1xx411c7mD?cid=11"},
		{"video:BV1xx411c7mD:12", "https://www.bilibili.com/video/BV1xx411c7mD?cid=12"},
		{"audio:11", "https://www.bilibili.com/audio/au11"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if got := online.ReferenceURL(bilibiliPolicy().Tracks, tc.id); got != tc.source {
				t.Fatalf("unexpected canonical URL: %q", got)
			}
			if err := online.ValidateTrack(bilibiliPolicy(), bilibiliTrack(tc.id, tc.source)); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, id := range []string{"11", "au11", "BV1xx411c7mD", "video:BV1xx411c7mD", "video:BV1xx411c7mD:0", "video:BV1xx411c7mD:01", "video:bv1xx411c7mD:11", "audio:0", "audio:01", "audio:-1", "audio:" + strings.Repeat("1", 21), "favorite:11", " audio:11", "audio:11?x=1"} {
		t.Run(id, func(t *testing.T) {
			if got := online.ReferenceURL(bilibiliPolicy().Tracks, id); got != "" {
				t.Fatalf("noncanonical identity accepted: %q", got)
			}
			if err := online.ValidateTrack(bilibiliPolicy(), bilibiliTrack(id, "https://www.bilibili.com/audio/au11")); err == nil {
				t.Fatal("noncanonical worker identity was accepted")
			}
		})
	}
}

func TestBilibiliSourceURLsCannotBeForgedWithFreshDigests(t *testing.T) {
	for _, source := range []string{
		"http://127.0.0.1/private", "https://www.bilibili.com.evil.invalid/audio/au11",
		"https://www.bilibili.com@evil.invalid/audio/au11", "https://www.bilibili.com/audio/au12",
		"https://www.bilibili.com/audio/au11?redirect=https://evil.invalid", "https://www.bilibili.com/audio/au11#fragment",
		"https://www.bilibili.com:443/audio/au11", "http://www.bilibili.com/audio/au11",
	} {
		t.Run(source, func(t *testing.T) {
			if err := online.ValidateTrack(bilibiliPolicy(), bilibiliTrack("audio:11", source)); err == nil {
				t.Fatal("forged source URL passed with a matching metadata digest")
			}
		})
	}
	track := bilibiliTrack("video:BV1xx411c7mD:11", "https://www.bilibili.com/video/BV1xx411c7mD?cid=12")
	if online.ValidateTrack(bilibiliPolicy(), track) == nil {
		t.Fatal("another video part was accepted as the same identity")
	}
}

func bilibiliObservation(id string) online.PlaylistObservation {
	count := int64(3)
	p := online.Playlist{ExternalID: id, Title: "Public collection", SourceURL: online.ReferenceURL(bilibiliPolicy().Playlists, id), TrackCount: &count}
	entries := []online.PlaylistEntry{{Position: 0, ExternalTrackID: "video:BV1xx411c7mD:11"}, {Position: 1, ExternalTrackID: "video:BV1xx411c7mD:12"}, {Position: 2, ExternalTrackID: "video:BV1xx411c7mD:11"}}
	return online.PlaylistObservation{Playlist: p, Entries: entries, Complete: true, Consistency: online.Unversioned, ObservedAt: time.Now().UTC(), MetadataDigest: online.PlaylistDigest(p, entries)}
}

func TestBilibiliPlaylistNamespacesAndOrderedOccurrences(t *testing.T) {
	for _, tc := range []struct{ id, source string }{
		{"favorite:91", "https://www.bilibili.com/medialist/detail/ml91"},
		{"audio-menu:91", "https://www.bilibili.com/audio/am91"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if got := online.ReferenceURL(bilibiliPolicy().Playlists, tc.id); got != tc.source {
				t.Fatalf("playlist namespace lost: %q", got)
			}
			observation := bilibiliObservation(tc.id)
			if err := online.ValidateObservation(bilibiliPolicy(), observation, tc.id); err != nil {
				t.Fatal(err)
			}
			observation.Entries[1], observation.Entries[2] = observation.Entries[2], observation.Entries[1]
			observation.Entries[1].Position, observation.Entries[2].Position = 1, 2
			if online.ValidateObservation(bilibiliPolicy(), observation, tc.id) == nil {
				t.Fatal("reordered duplicate occurrences reused the prior digest")
			}
		})
	}
	for _, id := range []string{"91", "ml91", "am91", "favorite:0", "favorite:091", "audio-menu:0", "audio:91", "favorite:91:1", "favorite:" + strings.Repeat("1", 21)} {
		if got := online.ReferenceURL(bilibiliPolicy().Playlists, id); got != "" {
			t.Fatalf("invalid playlist identity accepted: %s -> %s", id, got)
		}
	}
	for _, mutate := range []func(*online.PlaylistObservation){
		func(p *online.PlaylistObservation) { p.Playlist.SourceURL = "https://www.bilibili.com/audio/am91" },
		func(p *online.PlaylistObservation) { p.Playlist.SourceURL = "https://evil.invalid/91" },
		func(p *online.PlaylistObservation) { p.Entries[0].ExternalTrackID = "11" },
		func(p *online.PlaylistObservation) { p.Entries[0].ExternalTrackID = "favorite:11" },
	} {
		observation := bilibiliObservation("favorite:91")
		mutate(&observation)
		observation.MetadataDigest = online.PlaylistDigest(observation.Playlist, observation.Entries)
		if online.ValidateObservation(bilibiliPolicy(), observation, "favorite:91") == nil {
			t.Fatal("forged playlist result passed with a matching digest")
		}
	}
	if online.ValidateObservation(bilibiliPolicy(), bilibiliObservation("favorite:91"), "audio-menu:91") == nil {
		t.Fatal("different playlist namespaces shared a request identity")
	}
}

func bilibiliPolicy() online.ResourcePolicy {
	return online.ResourcePolicy{
		Tracks: []online.ReferenceRule{
			{IDPattern: `^video:(BV[0-9A-Za-z]{10}):([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/video/${1}?cid=${2}"},
			{IDPattern: `^audio:([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/audio/au${1}"},
		},
		Playlists: []online.ReferenceRule{
			{IDPattern: `^favorite:([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/medialist/detail/ml${1}"},
			{IDPattern: `^audio-menu:([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/audio/am${1}"},
		},
		PlaylistEntryPatterns: []string{`^unavailable:[0-9]{1,5}:[1-9][0-9]{0,19}$`},
		ImageHosts:            []string{"*.hdslb.com"},
	}
}

func TestDeclaredPlaylistPlaceholderRemainsNonPlayable(t *testing.T) {
	observation := bilibiliObservation("favorite:91")
	missing := "NOT_FOUND"
	page := online.PlaylistTracksPage{Playlist: observation.Playlist, Items: []online.PlaylistTrack{{Position: 0, TrackLookup: online.TrackLookup{ExternalID: "unavailable:0:91", ErrorCode: &missing}}}, MetadataDigest: observation.MetadataDigest, ObservedAt: observation.ObservedAt, Consistency: online.Unversioned}
	request := online.PlaylistTracksRequest{ExternalID: "favorite:91", Limit: 20}
	if err := online.ValidatePlaylistTracks(bilibiliPolicy(), page, request); err != nil {
		t.Fatal("declared placeholder rejected", err)
	}
	page.Items[0].ExternalID = "unknown:91"
	if online.ValidatePlaylistTracks(bilibiliPolicy(), page, request) == nil {
		t.Fatal("undeclared placeholder namespace accepted")
	}
	track := bilibiliTrack("unavailable:0:91", "https://www.bilibili.com/audio/au91")
	page.Items[0].TrackLookup = online.TrackLookup{ExternalID: track.ExternalID, Track: &track}
	if online.ValidatePlaylistTracks(bilibiliPolicy(), page, request) == nil {
		t.Fatal("placeholder became a playable track")
	}
}

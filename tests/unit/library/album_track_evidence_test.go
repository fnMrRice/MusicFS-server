package library_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

func TestAlbumTrackEvidenceMatchesOneOriginalRow(t *testing.T) {
	original := []music.AlbumTrackEvidence{
		{Title: "ＷＡＮ", ArtistNames: []string{"Soda纯白 / 林小暗", "Soda"}},
		{Title: "Tryggvason", ArtistNames: []string{"YUN"}},
	}
	for _, test := range []struct {
		name   string
		tracks []scraper.Track
		match  bool
	}{
		{"title and one credited artist", []scraper.Track{{Title: " wan ", Artist: "林小暗"}}, true},
		{"source artist alias", []scraper.Track{{Title: "WAN", Artist: "Guest & SODA"}}, true},
		{"localized track title and complete credit", []scraper.Track{{Title: "月光", Artist: "Guest",
			LocalizedTitles:      []scraper.LocalizedTitle{{Language: "en", Title: "WAN"}},
			LocalizedArtistNames: []scraper.LocalizedTitle{{Language: "zh", Title: "主唱 ／ 林小暗"}}}}, true},
		{"other source artist", []scraper.Track{{Title: "WAN", Artist: "YUN"}}, false},
		{"mixed candidate rows", []scraper.Track{{Title: "WAN", Artist: "Other"}, {Title: "Unknown", Artist: "林小暗"}}, false},
		{"composer only", []scraper.Track{{Title: "WAN", Composer: "林小暗"}}, false},
		{"partial artist name", []scraper.Track{{Title: "WAN", Artist: "林小"}}, false},
		{"wrong recording title", []scraper.Track{{Title: "WAN (Live)", Artist: "Soda"}}, false},
		{"no tracks", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := music.MatchesAlbumTracks(original, test.tracks); got != test.match {
				t.Fatalf("match = %t, want %t", got, test.match)
			}
		})
	}
}

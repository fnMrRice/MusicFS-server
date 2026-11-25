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

func TestAlbumTrackEvidenceCreditSeparators(t *testing.T) {
	for _, test := range []struct {
		credit string
		member string
		match  bool
	}{
		{"doriko feat.初音ミク", "初音ミク", true},
		{"doriko FT.初音ミク", "doriko", true},
		{"歌手甲feat.歌手乙", "歌手乙", true},
		{"Singer ft.Guest", "Guest", true},
		{"歌手甲&歌手乙", "歌手乙", true},
		{"歌手甲＋歌手乙", "歌手甲", true},
		{"歌手甲 ／歌手乙", "歌手乙", true},
		{"AC/DC&Queen", "AC/DC", true},
		{"AC/DC&Queen", "DC", false},
		{"R&B", "R", false},
		{"R&B", "R&B", true},
		{"A品牌 R&B", "B", false},
		{"Left.Field", "Field", false},
		{"Defeat.Guest", "Guest", false},
		{"C++", "C", false},
		{"歌手甲&歌手乙", "歌手", false},
	} {
		t.Run(test.credit+"_"+test.member, func(t *testing.T) {
			match := func(source, candidate string) bool {
				return music.MatchesAlbumTracks(
					[]music.AlbumTrackEvidence{{Title: "Song", ArtistNames: []string{source}}},
					[]scraper.Track{{Title: "Song", Artist: candidate}},
				)
			}
			if match(test.credit, test.member) != test.match || match(test.member, test.credit) != test.match {
				t.Fatalf("credit %q and member %q should match in either order = %t", test.credit, test.member, test.match)
			}
		})
	}
}

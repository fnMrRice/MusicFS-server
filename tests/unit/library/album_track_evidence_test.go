package library_test

import (
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

func TestAlbumTrackEvidencePreservesOrderedArtistSearchTerms(t *testing.T) {
	credit := "ＡＣ／ＤＣ＆Queen FT.Soda纯白"
	if got := slices.Collect(music.ArtistCreditMembers(credit)); !slices.Equal(got, []string{"AC/DC", "Queen", "Soda纯白"}) {
		t.Fatalf("artist search terms: %v", got)
	}
}

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

func TestAlbumTrackEvidenceVersionSuffix(t *testing.T) {
	const song = "キミと見た花 キミと見た空"
	for _, tc := range []struct {
		source, candidate string
		want              bool
	}{
		{" -full ver.-", " (full ver.)", true},
		{" （ＦＵＬＬ　ｖｅｒ．）", " -full ver.-", true},
		{" -short version-", " (short version)", true},
		{" -full ver.-", "", false},
		{" -full ver.-", " (short ver.)", false},
		{" -full ver.-", " (live ver.)", false},
		{" -full ver.-", " (Live)", false},
		{" -full ver.-", " (full ver)", false},
		{" -Garnet Star-", " (Garnet Star)", false},
		{" -full ver.- reprise", " (full ver.) reprise", false},
		{" -full ver.", " (full ver.)", false},
	} {
		t.Run(tc.source+"/"+tc.candidate, func(t *testing.T) {
			original := []music.AlbumTrackEvidence{{Title: song + tc.source, ArtistNames: []string{"東山奈央", "大地葉"}}}
			track := scraper.Track{Title: "Localized song", Artist: "東山奈央 / 大地葉",
				LocalizedTitles: []scraper.LocalizedTitle{{Language: "ja", Title: song + tc.candidate}}}
			if music.MatchesAlbumTracks(original, []scraper.Track{track}) != tc.want {
				t.Fatalf("unexpected version match between %q and %q", tc.source, tc.candidate)
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

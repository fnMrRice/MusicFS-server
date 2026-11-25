package api_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type sourceCreditAlbum struct {
	albumRankingFixture
	evidence []music.AlbumTrackEvidence
}

func (r sourceCreditAlbum) MusicAlbumTrackEvidence(context.Context, string, string, string) ([]music.AlbumTrackEvidence, error) {
	return r.evidence, nil
}

type creditSearchCatalog struct {
	autoAlbumCatalog
	album, artist, song string
	compilation         bool
	searches, reads     []string
}

func (p *creditSearchCatalog) Search(_ context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if field == "albumTracks" {
		p.reads = append(p.reads, q.ExternalID)
		artist := p.artist
		if q.ExternalID != "correct-edition" {
			artist = "Unrelated performer"
		}
		return []scraper.Item{{ID: q.ExternalID, Title: p.album, Tracks: []scraper.Track{{Title: p.song, Artist: artist}}}}, nil
	}
	if field != "albumDetails" || q.Album != p.album || (q.Artist != "" && q.Artist != p.artist) {
		return nil, fmt.Errorf("unexpected query %+v", q)
	}
	p.searches = append(p.searches, q.Artist)
	if (q.Artist == "") == p.compilation {
		if p.compilation {
			return []scraper.Item{{ID: "wrong-a", Title: p.album, Artist: p.artist},
				{ID: "correct-edition", Title: p.album, Artist: "Various Artists"}}, nil
		}
		return []scraper.Item{{ID: "decoy-1", Title: p.album}, {ID: "decoy-2", Title: p.album}, {ID: "decoy-3", Title: p.album},
			{ID: "correct-edition", Title: p.album, Artist: "林小暗 / " + p.artist}}, nil
	}
	var unrelated []scraper.Item
	for _, id := range []string{"wrong-a", "wrong-b", "wrong-c"} {
		unrelated = append(unrelated, scraper.Item{ID: id, Title: p.album, Artist: q.Artist})
	}
	return unrelated, nil
}

func TestAlbumAutoScrapeUsesSourceCreditAndFallsBackForCompilations(t *testing.T) {
	for _, tt := range []struct {
		album, song string
		compilation bool
	}{
		{"WAN", "WAN", false},
		{"Festival", "Opening Song", true},
	} {
		t.Run(tt.album, func(t *testing.T) {
			const artist = "Soda纯白"
			catalog := &creditSearchCatalog{autoAlbumCatalog: autoAlbumCatalog{id: "netease"},
				album: tt.album, artist: artist, song: tt.song, compilation: tt.compilation}
			svc, err := onlinescrape.New([]scraper.Plugin{catalog})
			if err != nil {
				t.Fatal(err)
			}
			target := music.AlbumScrapeTarget{Name: tt.album, Artist: "Stale album artist", SourcePluginID: "musicfs.plugin.source.netease"}
			repo := sourceCreditAlbum{albumRankingFixture{target: target}, []music.AlbumTrackEvidence{
				{Title: "Guest track", ArtistNames: []string{"Guest"}},
				{Title: "Guest track", ArtistNames: []string{"Guest"}},
				{Title: tt.song, ArtistNames: []string{artist + " / 林小暗"}},
				{Title: "Second track", ArtistNames: []string{artist, "林小暗"}},
			}}
			items, err := svc.SearchVerifiedAlbum(t.Context(), repo, "admin", "album", "")
			if err != nil || len(items) != 1 || items[0].ID != "correct-edition" || items[0].AlbumTracksToken == "" {
				t.Fatalf("verified release %+v, %v", items, err)
			}
			if !slices.Equal(catalog.searches, []string{artist, ""}) || len(catalog.reads) != 3 {
				t.Fatalf("artist searches %v, release reads %v", catalog.searches, catalog.reads)
			}
		})
	}
}

package api_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type numberedAlbumCatalog struct {
	autoAlbumCatalog
	reply, artist string
	queries       []scraper.Query
}

type wanAlbumFixture struct{ albumRankingFixture }

func (wanAlbumFixture) MusicAlbumTrackEvidence(context.Context, string, string, string) ([]music.AlbumTrackEvidence, error) {
	return []music.AlbumTrackEvidence{{Title: "WAN", ArtistNames: []string{"Soda纯白", "林小暗"}}}, nil
}

func (p *numberedAlbumCatalog) Search(_ context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	p.queries = append(p.queries, q)
	item := scraper.Item{ID: p.reply, Title: "WAN"}
	if field == "albumTracks" {
		item.Tracks = []scraper.Track{{Title: "WAN", Artist: p.artist}}
	}
	return []scraper.Item{item}, nil
}

func TestAlbumAutoScrapeChecksNativeReleaseAndOriginalTrack(t *testing.T) {
	for _, tt := range []struct {
		name, reply, artist string
		want                int
	}{
		{"same edition and one credited artist", "134193659", "林小暗", 1},
		{"same track on different edition", "232", "Soda纯白", 0},
		{"same title by different artist", "134193659", "YUN", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			other := &numberedAlbumCatalog{autoAlbumCatalog: autoAlbumCatalog{id: "musicbrainz"}}
			native := &numberedAlbumCatalog{autoAlbumCatalog: autoAlbumCatalog{id: "netease"}, reply: tt.reply, artist: tt.artist}
			svc, err := onlinescrape.New([]scraper.Plugin{other, native})
			if err != nil {
				t.Fatal(err)
			}
			repo := albumRankingFixture{target: music.AlbumScrapeTarget{Name: "WAN", SourcePluginID: "musicfs.plugin.source.localfiles",
				Release: &audiotypes.AlbumRelease{Namespace: "netease", ID: "134193659"}}}
			items, err := svc.SearchVerifiedAlbum(t.Context(), wanAlbumFixture{repo}, "admin", "wan", "")
			if err != nil || len(items) != tt.want {
				t.Fatalf("Unexpected candidates %v, %v", items, err)
			}
			if len(native.queries) == 0 || len(other.queries) != 0 {
				t.Fatalf("Unexpected native and text queries %v, %v", native.queries, other.queries)
			}
			for _, query := range native.queries {
				if query.ExternalID != "134193659" || query.Album != "" {
					t.Fatalf("Expected exact release query, got %+v", query)
				}
			}
		})
	}
}

func TestAlbumAutoScrapeUsesOtherConfiguredCatalogWhenNativePluginIsAbsent(t *testing.T) {
	mb := &numberedAlbumCatalog{autoAlbumCatalog: autoAlbumCatalog{id: "musicbrainz"}, reply: "mb-edition", artist: "Singer"}
	svc, err := onlinescrape.New([]scraper.Plugin{mb})
	if err != nil {
		t.Fatal(err)
	}
	repo := albumRankingFixture{target: music.AlbumScrapeTarget{Name: "WAN", SourcePluginID: "musicfs.plugin.source.localfiles",
		Release: &audiotypes.AlbumRelease{Namespace: "netease", ID: "134193659"}}}
	items, err := svc.SearchVerifiedAlbum(t.Context(), repo, "admin", "wan", "")
	if err != nil || len(items) != 1 || items[0].PluginID != "musicfs.scraper.musicbrainz" || len(mb.queries) == 0 || mb.queries[0].Album != "WAN" {
		t.Fatalf("Expected a verified name search from the available catalog, got %v, %v, %v", items, mb.queries, err)
	}
}

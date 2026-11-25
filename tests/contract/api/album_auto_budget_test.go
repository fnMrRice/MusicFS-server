package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type slowAlbumTrackLists struct{ autoAlbumCatalog }

func (p slowAlbumTrackLists) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if field == "albumTracks" && q.ExternalID != "good" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.autoAlbumCatalog.Search(ctx, field, q)
}

func TestAlbumAutoScrapeReturnsVerifiedEditionWhenLaterReadsTimeOut(t *testing.T) {
	target := music.AlbumScrapeTarget{Name: "月光紀行", Artist: "Singer"}
	catalog := slowAlbumTrackLists{autoAlbumCatalog{id: "musicbrainz"}}
	for _, id := range []string{"good", "slow-second", "slow-third"} {
		catalog.items = append(catalog.items, scraper.Item{ID: id, Title: target.Name, Artist: target.Artist})
	}
	service, err := onlinescrape.New([]scraper.Plugin{catalog})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	list, err := service.SearchVerifiedAlbum(ctx, albumRankingFixture{target: target}, "admin", "album", "")
	if err != nil || len(list) != 1 || list[0].ID != "good" || list[0].AlbumTracksToken == "" {
		t.Fatalf("verified editions: %+v, %v", list, err)
	}
}

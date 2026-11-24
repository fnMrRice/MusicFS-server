package bootstrap

import (
	"net/http"
	"os"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type onlineScraperRepository interface {
	works.Repository
	music.Repository
	settings.Repository
}

func configureOnlineScrapers(handler *api.Server, repository onlineScraperRepository, content *mediastore.ContentService, metadata *enrichmentworker.Service) (*onlinescrape.Service, error) {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	plugins := online.Catalog(client, os.Getenv("MUSICFS_ACOUSTID_API_KEY"))
	service, err := onlinescrape.NewWithSettings(plugins, repository)
	if err != nil {
		return nil, err
	}
	metadata.LyricsCatalog = service
	service.SetLocalLyricsLookup(metadata.LocalLyrics)
	handler.RegisterOnlineScrapers(service, repository, content, client)
	handler.RegisterOnlineAlbumScrapers(service, repository)
	return service, nil
}

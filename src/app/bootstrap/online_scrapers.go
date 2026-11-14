package bootstrap

import (
	"net/http"
	"os"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func configureOnlineScrapers(handler *api.Server, repository works.Repository, content *mediastore.ContentService) error {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	plugins := online.Catalog(client, os.Getenv("MUSICFS_ACOUSTID_API_KEY"))
	service, err := onlinescrape.New(plugins)
	if err != nil {
		return err
	}
	handler.RegisterOnlineScrapers(service, repository, content, client)
	return nil
}

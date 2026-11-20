package online

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type coverArchivePlugin struct{ client *http.Client }

func CoverArtArchive(client *http.Client) scraper.Plugin { return coverArchivePlugin{client} }

func (coverArchivePlugin) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: "musicfs.scraper.coverartarchive", Name: "Cover Art Archive", Type: "scraper",
		Fields: []string{"artwork"}, ImageHosts: []string{"coverartarchive.org", "archive.org", "*.archive.org"}, Available: true}
}

func (p coverArchivePlugin) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	if field != "artwork" {
		return nil, fault.Fail("INVALID_ARGUMENT", "unsupported Cover Art Archive field")
	}
	var err error
	q, err = albumPageQuery("musicfs.scraper.coverartarchive", field, q)
	if err != nil {
		return nil, err
	}
	var items []scraper.Item
	if id := strings.TrimSpace(q.ExternalID); id != "" {
		if !brainzID.MatchString(id) {
			return nil, fault.Fail("INVALID_ARGUMENT", "invalid MusicBrainz release identifier")
		}
		title := strings.TrimSpace(q.Album)
		if title == "" {
			title = id
		}
		items = []scraper.Item{{ID: id, Title: title, Album: q.Album, Artist: q.Artist}}
	} else {
		var err error
		items, err = MusicBrainz(p.client).Search(ctx, "albumDetails", q)
		if err != nil {
			return nil, err
		}
	}
	covers := make([]scraper.Item, 0, len(items))
	client := p.client
	if client == nil {
		client = http.DefaultClient
	}
	imageClient := *client
	hosts := p.Descriptor().ImageHosts
	imageClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 6 || !scraper.AllowsImageURL(req.URL.String(), hosts) {
			return errors.New("unsupported Cover Art Archive redirect")
		}
		return nil
	}
	for _, item := range items {
		imageURL := "https://coverartarchive.org/release/" + item.ID + "/front-500"
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, imageURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := catalogRequest(&imageClient, req)
		if errors.Is(err, errCatalogMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
		item.ImageURL, item.SourceURL = imageURL, brainzSite+"/release/"+item.ID+"/cover-art"
		covers = append(covers, item)
	}
	return covers, nil
}

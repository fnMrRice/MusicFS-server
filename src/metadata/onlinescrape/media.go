package onlinescrape

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// OpenMedia 只读取已选插件所声明的图片主机。每次重定向重新校验目标。
func (s *Service) OpenMedia(ctx context.Context, owner string, selected works.ScrapedField, client *http.Client) (io.ReadCloser, string, error) {
	if selected.Field == "lyrics" {
		return io.NopCloser(strings.NewReader(selected.Item.Lyrics)), "text/plain", nil
	}
	if selected.Field != "artwork" {
		return nil, "", domain.Fail("UNSUPPORTED", "Field is not track media")
	}
	p := s.plugins[selected.PluginID]
	if p == nil {
		return nil, "", domain.Fail("NOT_FOUND", "Unknown scraper plugin")
	}
	hosts := p.Descriptor().ImageHosts
	if !scraper.AllowsImageURL(selected.Item.ImageURL, hosts) {
		return nil, "", domain.Fail("UNSUPPORTED", "Unrecognized scraper image host")
	}
	record, err := s.currentSettings(ctx, owner, selected.PluginID)
	if err != nil {
		return nil, "", err
	}
	plugin, client, release, err := s.configured(selected.PluginID, record, client)
	if err != nil {
		return nil, "", err
	}
	var body io.ReadCloser
	var mime string
	if downloader, ok := plugin.(scraper.ArtworkDownloader); ok {
		body, mime, err = downloader.OpenArtwork(ctx, selected.Item.ImageURL)
	} else {
		body, mime, err = scraper.OpenImage(ctx, selected.Item.ImageURL, hosts, client)
	}
	if err != nil {
		release()
		return nil, "", err
	}
	return &configuredMediaBody{ReadCloser: body, release: release}, mime, nil
}

type configuredMediaBody struct {
	io.ReadCloser
	release func()
}

func (b *configuredMediaBody) Close() error { err := b.ReadCloser.Close(); b.release(); return err }

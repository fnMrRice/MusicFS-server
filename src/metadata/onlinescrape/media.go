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
	_, client, release, err := s.configured(selected.PluginID, record, client)
	if err != nil {
		return nil, "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 6 || !scraper.AllowsImageURL(req.URL.String(), hosts) {
			return domain.Fail("UNSUPPORTED", "Unsupported artwork redirect")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, selected.Item.ImageURL, nil)
	if err != nil {
		release()
		return nil, "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		release()
		return nil, "", domain.Fail("SOURCE_UNAVAILABLE", "Artwork request failed")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		release()
		return nil, "", domain.Fail("SOURCE_UNAVAILABLE", "Artwork unavailable")
	}
	return &configuredMediaBody{ReadCloser: resp.Body, release: release}, resp.Header.Get("Content-Type"), nil
}

type configuredMediaBody struct {
	io.ReadCloser
	release func()
}

func (b *configuredMediaBody) Close() error { err := b.ReadCloser.Close(); b.release(); return err }

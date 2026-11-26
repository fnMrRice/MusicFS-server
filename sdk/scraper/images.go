package scraper

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// ArtworkDownloader 使用插件自身的网络配置下载封面。
type ArtworkDownloader interface {
	OpenArtwork(context.Context, string) (io.ReadCloser, string, error)
}

// OpenImage 仅允许插件声明的图片主机，包括每次跳转的目标。
func OpenImage(ctx context.Context, raw string, hosts []string, client *http.Client) (io.ReadCloser, string, error) {
	if !AllowsImageURL(raw, hosts) {
		return nil, "", fault.Fail("UNSUPPORTED", "Unrecognized scraper image host")
	}
	if client == nil {
		client = http.DefaultClient
	}
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 6 || !AllowsImageURL(req.URL.String(), hosts) {
			return fault.Fail("UNSUPPORTED", "Unsupported artwork redirect")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/jpeg, image/png")
	response, err := c.Do(req)
	if err != nil {
		return nil, "", fault.Fail("SOURCE_UNAVAILABLE", "Artwork request failed")
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, "", fault.Fail("SOURCE_UNAVAILABLE", "Artwork unavailable")
	}
	return response.Body, response.Header.Get("Content-Type"), nil
}

// AllowsImageURL 校验图片及其重定向；通配符仅匹配声明域名的子域。
func AllowsImageURL(value string, hosts []string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Port() != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range hosts {
		if host == h || strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:]) {
			return true
		}
	}
	return false
}

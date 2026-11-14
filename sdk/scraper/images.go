package scraper

import (
	"net/url"
	"strings"
)

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

package bootstrap

import "strings"

// 显式列出可信来源对应的 WS 地址，兼容不将 connect-src self 匹配到 WS 的浏览器。
// 只使用已验证的部署配置，不从 Host 或转发头拼接安全策略。
func webContentPolicy(origins []string) string {
	connections := []string{"'self'"}
	for _, origin := range origins {
		if strings.HasPrefix(origin, "https://") {
			connections = append(connections, "wss://"+strings.TrimPrefix(origin, "https://"))
		} else if strings.HasPrefix(origin, "http://") {
			connections = append(connections, "ws://"+strings.TrimPrefix(origin, "http://"))
		}
	}
	return "default-src 'self'; img-src 'self' data: blob: https://p1.music.126.net https://p2.music.126.net https://p3.music.126.net https://p4.music.126.net; media-src 'self'; connect-src " + strings.Join(connections, " ") + "; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
}

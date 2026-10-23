package bootstrap

import (
	"slices"
	"strings"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
)

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
	// 图片来源只取受信任装配工厂的有效声明，不采用远端 Worker 响应或请求参数。
	hosts := []string{}
	for _, descriptor := range onlineplugins.Descriptors() {
		if descriptor.Validate(descriptor.Platform) != nil {
			continue
		}
		for _, host := range descriptor.Resources.ImageHosts {
			hosts = append(hosts, "https://"+host)
		}
	}
	slices.Sort(hosts)
	images := append([]string{"'self'", "data:", "blob:"}, slices.Compact(hosts)...)
	return "default-src 'self'; img-src " + strings.Join(images, " ") + "; media-src 'self'; connect-src " + strings.Join(connections, " ") + "; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
}

package library

import "github.com/fnMrRice/MusicFS-server/sdk/online"

// NetEaseArtworkURL 只接受网易云已规范化的 HTTPS 图片 CDN 地址。
func NetEaseArtworkURL(raw string) string {
	policy := online.ResourcePolicy{ImageHosts: []string{"*.music.126.net"}}
	value, err := policy.ImageURL(raw)
	if err != nil || value == nil || *value != raw {
		return ""
	}
	return *value
}

// Package online 装配各个在线刮削插件；每个插件自行声明所支持的资料。
package online

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func Catalog(client *http.Client, acoustIDKey string) []scraper.Plugin {
	factories := []func(*http.Client) scraper.Plugin{
		MusicBrainz, CoverArtArchive, LRCLIB, ITunes, Bangumi, DLsite, NetEase, QQMusic,
		func(client *http.Client) scraper.Plugin { return AcoustID(client, acoustIDKey) },
	}
	plugins := make([]scraper.Plugin, 0, len(factories))
	for _, factory := range factories {
		plugins = append(plugins, configuredPlugin(client, factory))
	}
	return plugins
}

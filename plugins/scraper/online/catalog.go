// Package online 装配各个在线刮削插件；每个插件自行声明所支持的资料。
package online

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

func Catalog(client *http.Client, acoustIDKey string) []scraper.Plugin {
	return []scraper.Plugin{
		MusicBrainz(client), CoverArtArchive(client), LRCLIB(client), ITunes(client),
		Bangumi(client), DLsite(client), AcoustID(client, acoustIDKey),
	}
}

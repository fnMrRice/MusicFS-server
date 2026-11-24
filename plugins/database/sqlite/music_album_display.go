package sqlite

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// 返回可折叠的标签所属实例。手工关联、编辑或刮削过的专辑具有独立身份。
const passiveAlbumProvider = `CASE WHEN a.origin='tag' AND a.metadata_locked=0 AND a.source_links='[]'
 AND NOT EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_manual_album_tracks m WHERE m.album_id=a.id)
 THEN coalesce(a.tag_provider,'') ELSE '' END`

type albumContext struct {
	albumID, providerID string
}

type albumCredit struct {
	id, name, provider string
}

// 同一曲目只展示一次被动来源标签。有独立资料的同名发行全部保留。
func displayAlbums[T any](albums []T, scope albumContext, credit func(T) albumCredit) []T {
	if len(albums) < 2 {
		return albums
	}
	type choice struct {
		id      string
		rank    int
		curated int
	}
	choices := make(map[string]choice, len(albums))
	keys := make([]string, len(albums))
	for i, album := range albums {
		c := credit(album)
		key := strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(c.name)), " "))
		keys[i] = key
		rank := 3
		switch {
		case c.id == scope.albumID:
			rank = 0
		case c.provider == "":
			rank = 1
		case c.provider == scope.providerID:
			rank = 2
		}
		current, exists := choices[key]
		if c.provider == "" {
			current.curated++
		}
		if !exists || rank < current.rank || rank == current.rank && c.id < current.id {
			current.id, current.rank = c.id, rank
		}
		choices[key] = current
	}
	result := albums[:0]
	for i, album := range albums {
		c := credit(album)
		group := choices[keys[i]]
		// 进入某张标签专辑时链接留在当前专辑；多张已确认的同名发行仍分别可见。
		if c.id == group.id || (group.curated > 1 && c.provider == "") {
			result = append(result, album)
		}
	}
	return result
}

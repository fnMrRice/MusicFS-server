package library

import "strings"

// NormalizeArtists 按斜杠拆分歌手署名，去除首尾空白和空项，并按首次出现顺序去重。
func NormalizeArtists(credits []string) []string {
	artists := []string{}
	seen := make(map[string]bool)
	for _, credit := range credits {
		for name := range strings.SplitSeq(credit, "/") {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			artists = append(artists, name)
		}
	}
	return artists
}

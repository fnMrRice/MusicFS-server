package policies

import "github.com/fnMrRice/MusicFS-server/sdk/online"

func Netease() online.ResourcePolicy {
	rule := func(kind string) []online.ReferenceRule {
		return []online.ReferenceRule{{IDPattern: `^([1-9][0-9]{0,19})$`, URLTemplate: "https://music.163.com/" + kind + "?id=${1}"}}
	}
	return online.ResourcePolicy{Tracks: rule("song"), Playlists: rule("playlist"), Albums: rule("album"), Artists: rule("artist"), ImageHosts: []string{"*.music.126.net"}}
}

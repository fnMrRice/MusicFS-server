package policies

import "github.com/fnMrRice/MusicFS-server/sdk/online"

func Bilibili() online.ResourcePolicy {
	return online.ResourcePolicy{
		Tracks: []online.ReferenceRule{
			{IDPattern: `^video:(BV[0-9A-Za-z]{10}):([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/video/${1}?cid=${2}"},
			{IDPattern: `^audio:([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/audio/au${1}"},
		},
		Playlists: []online.ReferenceRule{
			{IDPattern: `^favorite:([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/medialist/detail/ml${1}"},
			{IDPattern: `^audio-menu:([1-9][0-9]{0,19})$`, URLTemplate: "https://www.bilibili.com/audio/am${1}"},
		},
		PlaylistEntryPatterns: []string{`^unavailable:[0-9]{1,5}:[1-9][0-9]{0,19}$`},
		ImageHosts:            []string{"*.hdslb.com"},
	}
}

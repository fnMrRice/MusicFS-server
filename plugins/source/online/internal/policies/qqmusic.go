package policies

import "github.com/fnMrRice/MusicFS-server/sdk/online"

func QQMusic() online.ResourcePolicy {
	return online.ResourcePolicy{Tracks: []online.ReferenceRule{{IDPattern: `^([A-Za-z0-9]{1,64})$`, URLTemplate: "https://y.qq.com/n/ryqq/songDetail/${1}"}}, ImageHosts: []string{"*.y.qq.com", "*.qpic.cn"}}
}

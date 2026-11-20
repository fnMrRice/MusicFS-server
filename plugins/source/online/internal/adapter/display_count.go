package adapter

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
)

func accountDisplayCount(view AccountView) *plugindisplay.Count {
	count := &plugindisplay.Count{Unit: "users", LocalizedUnits: map[string]string{
		"zh": "个用户", "zh-Hans": "个用户", "zh-Hant": "個使用者", "ja": "ユーザー",
	}}
	if view.VerifiedAt(time.Now()) {
		count.Value = 1
	}
	return count
}

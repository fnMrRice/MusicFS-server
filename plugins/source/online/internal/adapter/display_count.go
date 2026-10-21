package adapter

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
)

func accountDisplayCount(view AccountView) *plugindisplay.Count {
	count := &plugindisplay.Count{Unit: "users", LocalizedUnits: map[string]string{
		"zh": "个用户", "zh-Hans": "个用户", "zh-Hant": "個使用者", "ja": "ユーザー",
	}}
	if view.State == "signedIn" && view.AccountID != "" && view.Revision != "" && (view.ExpiresAt == nil || time.Now().Before(*view.ExpiresAt)) {
		count.Value = 1
	}
	return count
}
